package anilist

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const relationsTTL = 7 * 24 * time.Hour

// Relation is another entry related to a media entry.
type Relation struct {
	Type  string `json:"type"` // AniList MediaRelation: SEQUEL, PREQUEL, SIDE_STORY, ...
	Kind  string `json:"kind"` // ANIME or MANGA
	Media Media  `json:"media"`
}

// Relations returns an entry's related entries, cached for a week.
func (c *Client) Relations(ctx context.Context, id int) ([]Relation, error) {
	key := "anilist:relations:" + strconv.Itoa(id)
	if c.cache != nil {
		if v, updated, ok, err := c.cache.GetKV(ctx, key); err == nil && ok && time.Since(updated) < relationsTTL {
			var rels []Relation
			if json.Unmarshal([]byte(v), &rels) == nil {
				return rels, nil
			}
		}
	}
	var data struct {
		Media *struct {
			Relations struct {
				Edges []struct {
					RelationType string `json:"relationType"`
					Node         struct {
						Type string `json:"type"`
						Media
					} `json:"node"`
				} `json:"edges"`
			} `json:"relations"`
		} `json:"Media"`
	}
	q := `query ($id: Int) { Media(id: $id, type: ANIME) { relations { edges { relationType node { type ` + mediaFields + ` } } } } }`
	if err := c.query(ctx, q, map[string]any{"id": id}, &data); err != nil {
		return nil, fmt.Errorf("anilist relations %d: %w", id, err)
	}
	if data.Media == nil {
		return nil, fmt.Errorf("anilist relations %d: %w", id, ErrNotFound)
	}
	rels := []Relation{}
	for _, e := range data.Media.Relations.Edges {
		rels = append(rels, Relation{Type: e.RelationType, Kind: e.Node.Type, Media: e.Node.Media})
	}
	if c.cache != nil {
		if b, err := json.Marshal(rels); err == nil {
			_ = c.cache.PutKV(ctx, key, string(b))
		}
	}
	return rels, nil
}

// Sequels picks the anime sequels (not music videos) from rels, earliest first.
func Sequels(rels []Relation) []Media { return related(rels, "SEQUEL") }

// Prequels picks the anime prequels the same way, so a show can be followed
// back to where it started.
func Prequels(rels []Relation) []Media { return related(rels, "PREQUEL") }

func related(rels []Relation, relation string) []Media {
	var out []Media
	for _, r := range rels {
		if r.Type == relation && r.Kind == "ANIME" && r.Media.Format != "MUSIC" && !r.Media.IsAdult {
			out = append(out, r.Media)
		}
	}
	return out
}
