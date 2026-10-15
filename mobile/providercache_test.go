package mobile

import (
	"testing"

	"github.com/EmoFa/tsuzuki/internal/anilist"
)

func TestMissingThumbnails(t *testing.T) {
	media := anilist.Media{ID: 1, Episodes: 2, Status: "FINISHED"}
	both := map[float64]anilist.EpisodeInfo{1: {Thumbnail: "a"}, 2: {Thumbnail: "b"}}
	if missingThumbnails(media, both) {
		t.Error("every aired episode has one")
	}
	if !missingThumbnails(media, map[float64]anilist.EpisodeInfo{1: {Thumbnail: "a"}, 2: {Title: "t"}}) {
		t.Error("episode 2 has none")
	}
	if !missingThumbnails(media, nil) {
		t.Error("AniList listed nothing")
	}
}
