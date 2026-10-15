package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/auth"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

func (a *App) Search(ctx context.Context, query string) ([]anilist.Media, error) {
	al, err := a.AniList(ctx)
	if err != nil {
		return nil, err
	}
	return al.Search(ctx, query, 25)
}

func (a *App) Browse(ctx context.Context, q anilist.BrowseQuery) (anilist.BrowsePage, error) {
	al, err := a.AniList(ctx)
	if err != nil {
		return anilist.BrowsePage{}, err
	}
	return al.Browse(ctx, q)
}

func (a *App) Genres(ctx context.Context) ([]string, error) {
	al, err := a.AniList(ctx)
	if err != nil {
		return nil, err
	}
	return al.Genres(ctx)
}

func (a *App) Media(ctx context.Context, id int) (anilist.Media, error) {
	al, err := a.AniList(ctx)
	if err != nil {
		return anilist.Media{}, err
	}
	return al.Media(ctx, id)
}

// StreamingEpisodes is AniList's titles and thumbnails for a show's episodes.
func (a *App) StreamingEpisodes(ctx context.Context, id int) (map[float64]anilist.EpisodeInfo, error) {
	al, err := a.AniList(ctx)
	if err != nil {
		return nil, err
	}
	return al.StreamingEpisodes(ctx, id)
}

// Sequels lists a show's anime sequels, from AniList.
func (a *App) Sequels(ctx context.Context, mediaID int) ([]anilist.Media, error) {
	al, err := a.AniList(ctx)
	if err != nil {
		return nil, err
	}
	rels, err := al.Relations(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	return anilist.Sequels(rels), nil
}

// Prequels lists a show's anime prequels, from AniList.
func (a *App) Prequels(ctx context.Context, mediaID int) ([]anilist.Media, error) {
	al, err := a.AniList(ctx)
	if err != nil {
		return nil, err
	}
	// Relations are cached, so asking twice for one show costs one request.
	rels, err := al.Relations(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	return anilist.Prequels(rels), nil
}

func (a *App) RecentShows(ctx context.Context, limit int) ([]store.Progress, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	return st.RecentShows(ctx, limit)
}

func (a *App) ShowProgress(ctx context.Context, mediaID int) ([]store.Progress, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	return st.ShowProgress(ctx, mediaID)
}

func (a *App) ListEntries(ctx context.Context, status string) ([]store.ListEntry, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	return st.ListEntries(ctx, status)
}

func (a *App) ListEntry(ctx context.Context, mediaID int) (*store.ListEntry, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	return st.ListEntry(ctx, mediaID)
}

func (a *App) ShowPrefs(ctx context.Context, mediaID int) (*store.ShowPrefs, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	return st.ShowPrefs(ctx, mediaID)
}

func (a *App) SaveShowPrefs(ctx context.Context, p store.ShowPrefs) error {
	st, err := a.Store(ctx)
	if err != nil {
		return err
	}
	return st.SaveShowPrefs(ctx, p)
}

func (a *App) DeleteShowPrefs(ctx context.Context, mediaID int) error {
	st, err := a.Store(ctx)
	if err != nil {
		return err
	}
	return st.DeleteShowPrefs(ctx, mediaID)
}

// WatchedBefore reports episodes completed in earlier watch-throughs.
func (a *App) WatchedBefore(ctx context.Context, mediaID int) (map[float64]bool, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	return st.WatchedBefore(ctx, mediaID)
}

// Round is the show's current watch-through (from 1) and how far it had been
// watched when that round started.
func (a *App) Round(ctx context.Context, mediaID int) (round, watchedBefore int, err error) {
	st, err := a.Store(ctx)
	if err != nil {
		return 0, 0, err
	}
	return st.RoundInfo(ctx, mediaID)
}

// EpisodeKinds classifies episodes (canon, filler, ...); nil when unknown.
func (a *App) EpisodeKinds(ctx context.Context, media anilist.Media) (map[int]skip.EpisodeKind, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	client, err := a.HTTP(ctx)
	if err != nil {
		return nil, err
	}
	return a.FillerList(client, st).Kinds(ctx, media)
}

// ProviderEpisodes lists episodes from the first provider that has the show.
// If it has no thumbnails, later providers are asked for them.
// Used when AniList doesn't know how many episodes have aired.
func (a *App) ProviderEpisodes(ctx context.Context, media anilist.Media, mode domain.Mode) ([]domain.Episode, error) {
	reg, err := a.Providers(ctx)
	if err != nil {
		return nil, err
	}
	mapper, err := a.Mapper(ctx)
	if err != nil {
		return nil, err
	}
	var errs []error
	var found []domain.Episode
	for _, name := range a.Config.ActiveProviders() {
		p, err := reg.Get(name)
		if err != nil {
			continue
		}
		show, err := mapper.Resolve(ctx, p, media, mode)
		if err == nil {
			var eps []domain.Episode
			if eps, err = p.Episodes(ctx, show.ID, mode); err == nil && len(eps) > 0 {
				if found == nil {
					found = eps
				} else {
					fillThumbnails(found, eps)
				}
				if hasThumbnails(found) {
					return found, nil
				}
				continue // this one has no thumbnails; a later provider may
			}
		}
		if err != nil && found == nil {
			errs = append(errs, err)
		}
	}
	if found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("no provider lists episodes for %s: %w", media.DisplayTitle(), errors.Join(errs...))
}

// Watch plays req, blocking until playback ends. onStatus may be called from
// any goroutine.
func (a *App) Watch(ctx context.Context, req session.Request, onStatus func(session.Status)) error {
	// A session per watch keeps each watch's status callback separate.
	sess, err := a.Session(ctx, onStatus)
	if err != nil {
		return err
	}
	return sess.Watch(ctx, req)
}

// Account describes the AniList login.
type Account struct {
	Backend  string // tracking.backend
	User     string
	LoggedIn bool
	Expired  bool
}

// Account reads the AniList login.
func (a *App) Account() Account {
	acc := Account{Backend: a.Config.Tracking.Backend}
	if tok, err := a.TokenFile().Load(); err == nil && tok != nil {
		acc.User = tok.UserName
		acc.LoggedIn = tok.Valid()
		acc.Expired = !tok.Valid()
	}
	return acc
}

// Login runs the AniList browser flow, verifies the token and stores it,
// returning the user name. show is told the approval page's URL first.
func (a *App) Login(ctx context.Context, show func(url string)) (string, error) {
	open := a.Platform.OpenURL
	if open == nil {
		open = auth.OpenBrowser
	}
	grant, err := auth.Login(ctx, auth.LoginOptions{
		ClientID: a.AniListClientID(),
		Open: func(url string) error {
			show(url)
			return open(url)
		},
	})
	if errors.Is(err, auth.ErrNoClientID) {
		return "", fmt.Errorf(`%w. Create one at https://anilist.co/settings/developer with redirect URL
%s, then set tracking.anilist_client_id in %s`, err, auth.RedirectURL, a.ConfigPath)
	}
	if err != nil {
		return "", err
	}
	al, err := a.AniList(ctx)
	if err != nil {
		return "", err
	}
	user, err := al.WithToken(grant.AccessToken).Viewer(ctx)
	if err != nil {
		return "", fmt.Errorf("checking the new login: %w", err)
	}
	tok := auth.Token{AccessToken: grant.AccessToken, UserID: user.ID, UserName: user.Name}
	if grant.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(grant.ExpiresIn)
	}
	if err := a.TokenFile().Save(tok); err != nil {
		return "", err
	}
	a.resetTracker()
	return user.Name, nil
}

// Logout forgets the AniList login. The local list is kept.
func (a *App) Logout() error {
	if err := a.TokenFile().Delete(); err != nil {
		return err
	}
	a.resetTracker()
	return nil
}

// Sync flushes queued list changes and pulls the remote list.
func (a *App) Sync(ctx context.Context) (string, error) {
	t, err := a.Tracker(ctx)
	if err != nil {
		return "", err
	}
	if t.Remote == nil {
		if a.Config.Tracking.Backend != "anilist" {
			return "", errors.New(`tracking.backend is "local"; nothing to sync`)
		}
		return "", errors.New("not logged in to AniList; run `tsuzuki login`")
	}
	sent, err := t.Flush(ctx)
	if err != nil {
		return "", fmt.Errorf("sending list changes: %w", err)
	}
	pulled, err := t.Pull(ctx)
	if err != nil {
		return "", fmt.Errorf("fetching your list: %w", err)
	}
	return fmt.Sprintf("Synced with AniList: sent %d change(s), fetched %d entries.", sent, pulled), nil
}

// SetListStatus changes a show's list status, returning a note for the user.
func (a *App) SetListStatus(ctx context.Context, mediaID int, status string) (string, error) {
	t, err := a.Tracker(ctx)
	if err != nil {
		return "", err
	}
	r, err := t.SetStatus(ctx, mediaID, status)
	if err != nil {
		return "", err
	}
	label := StatusLabel(r.Entry.Status)
	switch {
	case !r.Changed:
		return "Already " + label + ".", nil
	case r.Synced:
		return "Set to " + label + " on AniList.", nil
	case r.SyncErr != nil:
		return "Set to " + label + "; AniList sync pending: " + FirstLine(r.SyncErr.Error()), nil
	}
	return "Set to " + label + ".", nil
}

// StatusLabel is the friendly name for a list status.
func StatusLabel(s string) string { return tracker.Label(s) }

// SetScore rates a show on the list from 1 to 10, returning a note.
func (a *App) SetScore(ctx context.Context, mediaID int, score float64) (string, error) {
	t, err := a.Tracker(ctx)
	if err != nil {
		return "", err
	}
	r, err := t.SetScore(ctx, mediaID, score)
	if err != nil {
		return "", err
	}
	return ScoreNote(r), nil
}

// ScoreNote describes a SetScore result.
func ScoreNote(r tracker.Result) string {
	score := strconv.FormatFloat(r.Entry.Score, 'f', -1, 64)
	switch {
	case !r.Changed:
		return "Already rated " + score + "/10."
	case r.Synced:
		return "Rated " + score + "/10 on AniList."
	case errors.Is(r.SyncErr, anilist.ErrUnauthorized):
		return "Rated " + score + "/10; AniList login expired, run `tsuzuki login` to sync."
	case r.SyncErr != nil:
		return "Rated " + score + "/10; AniList sync pending: " + FirstLine(r.SyncErr.Error())
	}
	return "Rated " + score + "/10."
}

// Rewatch is what starting a rewatch did.
type Rewatch struct {
	Round  int
	Result tracker.Result
}

// Note describes the rewatch for the user.
func (r Rewatch) Note() string {
	note := fmt.Sprintf("Rewatch started (round %d)", r.Round)
	switch {
	case r.Result.Synced:
		return note + "; AniList set to rewatching."
	case r.Result.SyncErr != nil:
		return note + "; AniList sync pending: " + FirstLine(r.Result.SyncErr.Error())
	}
	return note + "."
}

// StartRewatch begins another watch-through: progress counts from episode 1
// again and the list says Rewatching.
func (a *App) StartRewatch(ctx context.Context, mediaID int) (Rewatch, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return Rewatch{}, err
	}
	media, err := a.Media(ctx, mediaID)
	if err != nil {
		return Rewatch{}, err
	}
	through, err := a.WatchedThrough(ctx, media)
	if err != nil {
		return Rewatch{}, err
	}
	round, err := st.StartRound(ctx, mediaID, through)
	if err != nil {
		return Rewatch{}, err
	}
	t, err := a.Tracker(ctx)
	if err != nil {
		return Rewatch{}, err
	}
	r, err := t.StartRewatch(ctx, mediaID)
	if err != nil {
		return Rewatch{}, err
	}
	return Rewatch{Round: round, Result: r}, nil
}

// WatchedThrough is how far a show has been watched: the furthest episode
// completed here, or what the user's list says, whichever is further. It's the
// snapshot a rewatch keeps so earlier episodes still show as watched.
func (a *App) WatchedThrough(ctx context.Context, media anilist.Media) (int, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return 0, err
	}
	through := 0
	eps, err := st.ShowProgress(ctx, media.ID)
	if err != nil {
		return 0, err
	}
	for _, p := range eps {
		if p.Completed && int(p.Episode) > through {
			through = int(p.Episode)
		}
	}
	entry, err := st.ListEntry(ctx, media.ID)
	if err != nil {
		return 0, err
	}
	if entry != nil {
		listThrough := entry.Progress
		if entry.Status == tracker.Completed && media.Episodes > listThrough {
			listThrough = media.Episodes
		}
		if listThrough > through {
			through = listThrough
		}
	}
	return through, nil
}

// SetWatched marks a range of episodes watched or not, and brings the user's
// list into step. A list holds one progress number, so it can only say how far
// an unbroken run from the first episode reaches: marking an episode with
// earlier ones missing leaves the list where it is, and unmarking one pulls it
// back to just before that episode.
func (a *App) SetWatched(ctx context.Context, media anilist.Media, from, to float64, watched bool) (string, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return "", err
	}
	for ep := from; ep <= to; ep++ {
		if err := st.SetWatched(ctx, media.ID, ep, watched); err != nil {
			return "", err
		}
	}
	what := session.EpisodeRange(from, to)
	note := fmt.Sprintf("Marked %s as watched.", what)
	if !watched {
		note = fmt.Sprintf("Unmarked %s.", what)
	}

	entry, err := st.ListEntry(ctx, media.ID)
	if err != nil {
		return "", err
	}
	round, _, err := st.RoundInfo(ctx, media.ID)
	if err != nil {
		return "", err
	}
	was := tracker.ListThrough(entry, media.Episodes, round)
	want, err := a.listProgress(ctx, media, was)
	if err != nil {
		return "", err
	}
	if !watched {
		// The run now stops at the hole just made.
		want = min(want, int(from)-1)
	}
	if want == was {
		if watched && want < int(to) {
			return note + fmt.Sprintf(" Your list still says %s: %s aren't watched yet.",
				episodeCount(was), session.EpisodeRange(float64(was+1), from-1)), nil
		}
		return note, nil
	}

	t, err := a.Tracker(ctx)
	if err != nil {
		return "", err
	}
	r, err := t.SetProgress(ctx, media, want)
	if err != nil {
		return note + " Updating your list failed: " + FirstLine(err.Error()), nil
	}
	if errors.Is(r.SyncErr, anilist.ErrUnauthorized) {
		return note + " AniList login expired, run `tsuzuki login` to sync.", nil
	}
	if watched {
		return note + " " + upperFirst(r.Note()) + ".", nil
	}
	// Unmarking pulled the list back, which is the part worth saying.
	note += fmt.Sprintf(" Your list now says %s.", episodeCount(r.Entry.Progress))
	switch {
	case r.Synced:
		note += " AniList updated."
	case r.SyncErr != nil:
		note += " AniList sync pending: " + FirstLine(r.SyncErr.Error())
	}
	return note, nil
}

// listProgress is how far the list can say a show has been watched: the end of
// the unbroken run of episodes watched in this round, counting what the list
// already knows about as watched.
func (a *App) listProgress(ctx context.Context, media anilist.Media, through int) (int, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return 0, err
	}
	eps, err := st.ShowProgress(ctx, media.ID)
	if err != nil {
		return 0, err
	}
	watched := map[int]bool{}
	for _, p := range eps {
		if p.Completed {
			watched[int(p.Episode)] = true
		}
	}
	return tracker.WatchedRun(watched, through, media.Episodes), nil
}

// episodeCount reads as "9 episodes watched", for notes about the list.
func episodeCount(n int) string {
	if n == 1 {
		return "1 episode watched"
	}
	return fmt.Sprintf("%d episodes watched", n)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// FirstLine shortens an error to its first line, for one-line notes.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	if len(line) > 160 {
		line = line[:160] + "…"
	}
	return line
}

func hasThumbnails(eps []domain.Episode) bool {
	for _, e := range eps {
		if e.Thumbnail != "" {
			return true
		}
	}
	return false
}

// fillThumbnails copies thumbnails from other onto eps with the same number.
func fillThumbnails(eps, other []domain.Episode) {
	byNumber := make(map[float64]string, len(other))
	for _, e := range other {
		byNumber[e.Number] = e.Thumbnail
	}
	for i := range eps {
		if eps[i].Thumbnail == "" {
			eps[i].Thumbnail = byNumber[eps[i].Number]
		}
	}
}
