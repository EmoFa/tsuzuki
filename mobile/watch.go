package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/EmoFa/tsuzuki/internal/player"
	"github.com/EmoFa/tsuzuki/internal/session"
)

// Player is the app's video player, as tsuzuki drives it. Times are
// nanoseconds.
type Player interface {
	SeekTo(pos int64) error
	// ShowText shows a short message over the video for d.
	ShowText(text string, d int64) error
	// AddSubtitle adds a subtitle track without selecting it.
	AddSubtitle(url, title, lang string) error
	// BindKey offers an action, such as a "Skip" button, that reports message
	// through Playback.Message when used. key is the desktop key, as a hint.
	BindKey(key, message string) error
	// Stop ends playback. The player still reports Playback.Ended.
	Stop() error
}

// Playback is how the app's player reports to tsuzuki while it plays: often
// with Position, on each change with the others, and once with Ended. Times
// are nanoseconds.
type Playback struct {
	remote *player.Remote
}

func (p *Playback) Position(pos, duration int64) {
	p.remote.Position(time.Duration(pos), time.Duration(duration))
}

func (p *Playback) Duration(d int64)       { p.remote.Duration(time.Duration(d)) }
func (p *Playback) Paused(paused bool)     { p.remote.Paused(paused) }
func (p *Playback) Seeked(pos int64)       { p.remote.Seeked(time.Duration(pos)) }
func (p *Playback) Message(message string) { p.remote.Message(message) }

// Ended reports that playback is over: reason is "eof" when the episode played
// to its end, "quit" when the user left, or "error" with errMessage saying why.
func (p *Playback) Ended(reason, errMessage string) {
	var err error
	if errMessage != "" {
		err = errors.New(errMessage)
	}
	p.remote.Ended(reason, err)
}

// playerRef is the Player a Remote drives, known once Host.Play returns.
type playerRef struct{ p Player }

func (r *playerRef) Seek(pos time.Duration) error { return r.p.SeekTo(int64(pos)) }
func (r *playerRef) ShowText(text string, d time.Duration) error {
	return r.p.ShowText(text, int64(d))
}
func (r *playerRef) AddSubtitle(url, title, lang string) error {
	return r.p.AddSubtitle(url, title, lang)
}
func (r *playerRef) BindKey(key, message string) error { return r.p.BindKey(key, message) }
func (r *playerRef) Stop() error                       { return r.p.Stop() }

// play opens the app's player for req.
func (a *App) play(_ context.Context, req player.Request) (session.Playback, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ref := &playerRef{}
	pb := &Playback{remote: player.NewRemote(ref)}
	p, err := a.host.Play(string(b), pb)
	if err != nil {
		return nil, err
	}
	ref.p = p
	return pb.remote, nil
}

// StatusListener receives a watch's progress, as Status JSON.
type StatusListener interface {
	Status(statusJSON string)
}

// status is a session.Status for the app: Kind is named, and Err is a message.
type status struct {
	session.Status
	Kind string
	Err  string `json:",omitempty"`
}

var statusKinds = map[session.StatusKind]string{
	session.StatusResolving:      "resolving",
	session.StatusProviderFailed: "provider_failed",
	session.StatusPlaying:        "playing",
	session.StatusProgress:       "progress",
	session.StatusWatched:        "watched",
	session.StatusTracked:        "tracked",
	session.StatusSkipped:        "skipped",
	session.StatusEpisodeSkipped: "episode_skipped",
	session.StatusStopped:        "stopped",
	session.StatusNoNextEpisode:  "no_next_episode",
	session.StatusFinishedShow:   "finished_show",
	session.StatusSkipPrompt:     "skip_prompt",
}

// SkipMessage is what Playback.Message reports when the user asks to skip.
const SkipMessage = session.SkipMessage

func statusJSON(st session.Status) string {
	s := status{Status: st, Kind: statusKinds[st.Kind]}
	if st.Err != nil {
		s.Err = st.Err.Error()
	}
	b, err := json.Marshal(s)
	if err != nil {
		b, _ = json.Marshal(map[string]string{"Kind": "error", "Err": err.Error()})
	}
	return string(b)
}

// Watching is a watch in progress.
type Watching struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// Cancel stops the watch, closing the player.
func (w *Watching) Cancel() { w.cancel() }

// Await blocks until the watch ends: the user stopped, autoplay ran out of
// episodes, or it was cancelled (which isn't an error).
func (w *Watching) Await() error {
	<-w.done
	return w.err
}

// Watch plays a session.Request in the app's player: the requested episode,
// then with autoplay the ones after it. listener hears about its progress.
func (a *App) Watch(requestJSON string, listener StatusListener) (*Watching, error) {
	req, err := decode[session.Request](requestJSON)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	w := &Watching{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		defer cancel()
		err := a.core.Watch(ctx, req, func(st session.Status) { listener.Status(statusJSON(st)) })
		if ctx.Err() == nil {
			w.err = err
		}
	}()
	return w, nil
}
