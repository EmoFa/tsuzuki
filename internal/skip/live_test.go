//go:build live

package skip

import (
	"context"
	"testing"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/httpx"
)

func TestLive(t *testing.T) {
	ctx := context.Background()
	client := httpx.New(httpx.Options{})

	ranges, err := (&AniSkip{Client: client}).Ranges(ctx, 59978, 1, 24*time.Minute)
	if err != nil || len(ranges) == 0 {
		t.Errorf("aniskip Frieren S2 ep1: %+v err=%v", ranges, err)
	}

	shippuden := anilist.Media{ID: 1735, Title: anilist.Title{English: "Naruto Shippuden", Romaji: "Naruto: Shippuuden"}}
	kinds, err := (&FillerList{Client: client}).Kinds(ctx, shippuden)
	if err != nil || len(kinds) < 400 {
		t.Fatalf("fillerlist Naruto Shippuden: %d kinds err=%v", len(kinds), err)
	}
	// Report the first filler run, for manual autoplay testing.
	for n := 1; n <= len(kinds); n++ {
		if kinds[n] == Filler {
			end := n
			for kinds[end+1] == Filler {
				end++
			}
			t.Logf("first filler run: episodes %d-%d (episode %d is %s)", n, end, end+1, kinds[end+1])
			break
		}
	}
}
