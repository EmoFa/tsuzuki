package tracker

import (
	"context"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/store"
)

// ListThrough is how far a list entry says a show has been watched in its
// current round: its progress, the whole show when the list calls it completed,
// and nothing during a rewatch unless the entry describes that rewatch.
func ListThrough(e *store.ListEntry, episodes, round int) int {
	if e == nil {
		return 0
	}
	if round > 1 && e.Status != Repeating {
		return 0
	}
	through := e.Progress
	if e.Status == Completed && episodes > through {
		through = episodes
	}
	return through
}

// WatchedRun is how far an unbroken run of watched episodes reaches, starting
// from through (what the list already counts). A list holds one progress
// number, so a run with a hole in it can't be described past the hole.
func WatchedRun(watched map[int]bool, through, episodes int) int {
	for watched[through+1] {
		through++
		if episodes > 0 && through >= episodes {
			break
		}
	}
	return through
}

// SetProgress says how far a show has been watched, in either direction: marks
// made by hand are what the list should say, so unmarking an episode pulls the
// list back to before it, and a completed show becomes one being watched.
func (t *Tracker) SetProgress(ctx context.Context, media anilist.Media, progress int) (Result, error) {
	cur, err := t.Store.ListEntry(ctx, media.ID)
	if err != nil {
		return Result{}, err
	}
	e := store.ListEntry{MediaID: media.ID, Status: Current, Progress: max(progress, 0)}
	if cur != nil {
		e.Score = cur.Score
		if cur.Status == Repeating {
			e.Status = Repeating
		}
	}
	if media.Episodes > 0 && e.Progress >= media.Episodes {
		e.Status = Completed
	}
	if cur != nil && cur.Status == e.Status && cur.Progress == e.Progress {
		return Result{Entry: *cur}, nil
	}
	return t.save(ctx, e)
}
