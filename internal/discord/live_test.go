//go:build live

package discord

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLivePresence shows a sample activity in the running Discord client for a
// few seconds. It needs ANITUI_DISCORD_CLIENT_ID because it's visible to others.
func TestLivePresence(t *testing.T) {
	id := os.Getenv("ANITUI_DISCORD_CLIENT_ID")
	if id == "" {
		t.Skip("set ANITUI_DISCORD_CLIENT_ID to show a test presence")
	}
	ctx := context.Background()
	c, err := Dial(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	t.Logf("connected as %+v", c.User)
	start := time.Now().Add(-3 * time.Minute)
	err = c.SetActivity(ctx, &Activity{
		Type: Watching, StatusDisplayType: StatusDisplayDetails,
		Details: "Frieren: Beyond Journey’s End", DetailsURL: "https://anilist.co/anime/154587",
		State:      "Episode 3 of 28",
		Timestamps: &Timestamps{Start: start.UnixMilli(), End: start.Add(24 * time.Minute).UnixMilli()},
		Assets:     &Assets{LargeImage: "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx154587-gHSraOSa0nBG.jpg", LargeText: "Sousou no Frieren"},
		Buttons:    []Button{{Label: "View on AniList", URL: "https://anilist.co/anime/154587"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Second)
	if err := c.SetActivity(ctx, nil); err != nil {
		t.Fatal(err)
	}
}
