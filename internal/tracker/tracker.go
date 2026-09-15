// Package tracker keeps the user's anime list: always locally, and mirrored to
// AniList when logged in. Remote writes go through a durable queue so nothing
// is lost while offline.
package tracker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/store"
)

// List statuses, matching AniList's MediaListStatus.
const (
	Current   = "CURRENT"
	Planning  = "PLANNING"
	Completed = "COMPLETED"
	Dropped   = "DROPPED"
	Paused    = "PAUSED"
	Repeating = "REPEATING"
)

// Statuses in the order the UI shows them.
var Statuses = []string{Current, Planning, Completed, Paused, Dropped, Repeating}

// remoteTimeout bounds a single remote write.
const remoteTimeout = 15 * time.Second

type Store interface {
	ListEntry(ctx context.Context, mediaID int) (*store.ListEntry, error)
	SaveListEntry(ctx context.Context, e store.ListEntry) error
	ListEntries(ctx context.Context, status string) ([]store.ListEntry, error)
	QueueSync(ctx context.Context, mediaID int, status string, progress int) error
	SyncFailed(ctx context.Context, mediaID int, cause error) error
	SyncDone(ctx context.Context, p store.PendingSync) error
	PendingSyncs(ctx context.Context) ([]store.PendingSync, error)
}

// Remote is a list service such as AniList.
type Remote interface {
	SaveListEntry(ctx context.Context, mediaID int, status string, progress int) error
	UserList(ctx context.Context) ([]anilist.ListItem, error)
}

type Tracker struct {
	Store  Store
	Remote Remote // nil keeps the list local only
}

// Result describes what an update did.
type Result struct {
	Entry   store.ListEntry
	Changed bool  // the list changed
	Synced  bool  // the change reached the remote
	SyncErr error // why it didn't; the change stays queued
}

// Note summarises a result for display.
func (r Result) Note() string {
	progress := fmt.Sprintf("episode %d", r.Entry.Progress)
	switch {
	case !r.Changed:
		return "list already up to date"
	case r.Synced:
		return fmt.Sprintf("AniList updated: %s (%s)", progress, r.Entry.Status)
	case r.SyncErr != nil:
		return fmt.Sprintf("list updated to %s; AniList sync pending: %v", progress, r.SyncErr)
	}
	return fmt.Sprintf("list updated: %s (%s)", progress, r.Entry.Status)
}

// EpisodeWatched records a finished episode. Progress never goes backwards, so
// rewatching an earlier episode leaves the list alone.
func (t *Tracker) EpisodeWatched(ctx context.Context, media anilist.Media, episode float64) (Result, error) {
	progress := int(math.Floor(episode))
	cur, err := t.Store.ListEntry(ctx, media.ID)
	if err != nil {
		return Result{}, err
	}
	if cur != nil && progress <= cur.Progress {
		return Result{Entry: *cur}, nil
	}

	e := store.ListEntry{MediaID: media.ID, Status: Current, Progress: progress}
	if cur != nil {
		e.Score = cur.Score
		if cur.Status == Repeating {
			e.Status = Repeating
		}
	}
	if media.Episodes > 0 && progress >= media.Episodes {
		e.Status = Completed
	}
	return t.save(ctx, e)
}

// SetStatus changes a show's status, keeping its progress.
func (t *Tracker) SetStatus(ctx context.Context, mediaID int, status string) (Result, error) {
	cur, err := t.Store.ListEntry(ctx, mediaID)
	if err != nil {
		return Result{}, err
	}
	e := store.ListEntry{MediaID: mediaID, Status: status}
	if cur != nil {
		if cur.Status == status {
			return Result{Entry: *cur}, nil
		}
		e.Progress, e.Score = cur.Progress, cur.Score
	}
	return t.save(ctx, e)
}

func (t *Tracker) save(ctx context.Context, e store.ListEntry) (Result, error) {
	e.UpdatedAt = time.Now()
	if err := t.Store.SaveListEntry(ctx, e); err != nil {
		return Result{}, err
	}
	r := Result{Entry: e, Changed: true}
	if t.Remote == nil {
		return r, nil
	}
	if err := t.Store.QueueSync(ctx, e.MediaID, e.Status, e.Progress); err != nil {
		return r, err
	}
	pushed, err := t.Flush(ctx)
	r.Synced = err == nil && pushed > 0
	if !r.Synced {
		r.SyncErr = err
		if err == nil {
			r.SyncErr = errors.New("not sent")
		}
	}
	return r, nil
}

// Flush pushes every queued change, oldest first, returning how many were sent.
// An authorization failure stops early without counting as an attempt.
func (t *Tracker) Flush(ctx context.Context) (int, error) {
	if t.Remote == nil {
		return 0, nil
	}
	pending, err := t.Store.PendingSyncs(ctx)
	if err != nil {
		return 0, err
	}
	sent := 0
	var errs []error
	for _, p := range pending {
		pushCtx, cancel := context.WithTimeout(ctx, remoteTimeout)
		err := t.Remote.SaveListEntry(pushCtx, p.MediaID, p.Status, p.Progress)
		cancel()
		switch {
		case err == nil:
			if err := t.Store.SyncDone(ctx, p); err != nil {
				return sent, err
			}
			sent++
		case errors.Is(err, anilist.ErrUnauthorized), ctx.Err() != nil:
			return sent, err
		default:
			slog.Warn("list sync failed", "media", p.MediaID, "attempts", p.Attempts+1, "err", err)
			if serr := t.Store.SyncFailed(ctx, p.MediaID, err); serr != nil {
				slog.Warn("recording list sync failure", "media", p.MediaID, "err", serr)
			}
			errs = append(errs, err)
		}
	}
	return sent, errors.Join(errs...)
}

// Pull copies the remote list into the local one. Shows with changes still
// waiting to be pushed keep their local state.
func (t *Tracker) Pull(ctx context.Context) (int, error) {
	if t.Remote == nil {
		return 0, nil
	}
	items, err := t.Remote.UserList(ctx)
	if err != nil {
		return 0, err
	}
	pending, err := t.Store.PendingSyncs(ctx)
	if err != nil {
		return 0, err
	}
	skip := map[int]bool{}
	for _, p := range pending {
		skip[p.MediaID] = true
	}
	n := 0
	for _, it := range items {
		if skip[it.MediaID] {
			continue
		}
		e := store.ListEntry{MediaID: it.MediaID, Status: it.Status, Progress: it.Progress, Score: it.Score, UpdatedAt: it.UpdatedAt}
		if err := t.Store.SaveListEntry(ctx, e); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// AniListRemote adapts an authenticated AniList client for one user.
type AniListRemote struct {
	Client *anilist.Client
	UserID int
}

func (r AniListRemote) SaveListEntry(ctx context.Context, mediaID int, status string, progress int) error {
	return r.Client.SaveListEntry(ctx, mediaID, status, progress)
}

func (r AniListRemote) UserList(ctx context.Context) ([]anilist.ListItem, error) {
	return r.Client.UserList(ctx, r.UserID)
}
