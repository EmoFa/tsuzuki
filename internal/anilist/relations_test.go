package anilist

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRelationsAndSequels(t *testing.T) {
	calls := 0
	c, _ := server(t, func(w http.ResponseWriter, body string) {
		calls++
		w.Write(fixture(t, "relations_154587.json"))
	})
	ctx := context.Background()
	for range 2 {
		rels, err := c.Relations(ctx, 154587)
		if err != nil || len(rels) != 6 {
			t.Fatalf("relations = %d, %v", len(rels), err)
		}
		sequels := Sequels(rels)
		if len(sequels) != 1 || sequels[0].ID != 182255 || sequels[0].Episodes == 0 {
			t.Fatalf("sequels = %+v", sequels)
		}
	}
	if calls != 1 {
		t.Errorf("fetched %d times, want 1 (cached)", calls)
	}

	// Music videos and manga aren't sequels to watch.
	rels := []Relation{
		{Type: "SEQUEL", Kind: "ANIME", Media: Media{ID: 1, Format: "MUSIC"}},
		{Type: "SEQUEL", Kind: "MANGA", Media: Media{ID: 2, Format: "MANGA"}},
		{Type: "PREQUEL", Kind: "ANIME", Media: Media{ID: 3, Format: "TV"}},
		{Type: "SEQUEL", Kind: "ANIME", Media: Media{ID: 4, Format: "MOVIE"}},
	}
	if s := Sequels(rels); len(s) != 1 || s[0].ID != 4 {
		t.Errorf("filtered = %+v", s)
	}
}

func TestSaveListEntryScore(t *testing.T) {
	var vars []map[string]any
	c, _ := server(t, func(w http.ResponseWriter, body string) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.Unmarshal([]byte(body), &req)
		if !strings.Contains(req.Query, "scoreRaw: $scoreRaw") {
			t.Errorf("query = %s", req.Query)
		}
		vars = append(vars, req.Variables)
		w.Write([]byte(`{"data":{"SaveMediaListEntry":{"id":1,"status":"COMPLETED","progress":28}}}`))
	})
	ctx := context.Background()
	score := 8.5
	if err := c.SaveListEntry(ctx, 154587, "COMPLETED", 28, &score); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveListEntry(ctx, 154587, "COMPLETED", 28, nil); err != nil {
		t.Fatal(err)
	}
	if vars[0]["scoreRaw"] != float64(85) {
		t.Errorf("scoreRaw = %v", vars[0]["scoreRaw"])
	}
	if _, ok := vars[1]["scoreRaw"]; ok {
		t.Errorf("unset score sent: %v", vars[1])
	}
}
