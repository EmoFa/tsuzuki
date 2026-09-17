package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

type (
	finishEntryMsg   struct{ entry *store.ListEntry }
	finishSequelsMsg struct {
		sequels []anilist.Media
		err     error
	}
	finishScoredMsg struct {
		note string
		err  error
	}
)

// finishPanel follows the last episode of a show: rate it, then maybe start
// the sequel.
type finishPanel struct {
	p       *playingScreen
	rating  bool // first step; false once rated or skipped
	entry   *store.ListEntry
	sequels []anilist.Media
	loaded  bool
	err     error
	cursor  int
}

func (p *playingScreen) startFinishing() tea.Cmd {
	f := &finishPanel{p: p, rating: true}
	p.finish = f
	ctx, svc, media := p.ctx, p.svc, p.req.Media
	return tea.Batch(
		func() tea.Msg {
			e, err := svc.ListEntry(ctx, media.ID)
			if err != nil {
				return nil
			}
			return finishEntryMsg{e}
		},
		func() tea.Msg {
			s, err := svc.Sequels(ctx, media.ID)
			return finishSequelsMsg{s, err}
		},
	)
}

var (
	keyRate      = key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9", "0"), key.WithHelp("1-9, 0", "rate (0 = 10)"))
	keySkipRate  = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "skip"))
	keyWatchNext = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "watch sequel"))
	keyPlanNext  = key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "add to Planning"))
	keyNextInfo  = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details"))
	keyDone      = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "done"))
)

func (f *finishPanel) help() []key.Binding {
	if f.rating {
		return []key.Binding{keyRate, keySkipRate, keyDone}
	}
	if len(f.sequels) > 0 {
		return []key.Binding{keyWatchNext, keyPlanNext, keyNextInfo, keyDone}
	}
	return []key.Binding{keyDone}
}

func (f *finishPanel) update(msg tea.Msg) tea.Cmd {
	p := f.p
	switch msg := msg.(type) {
	case finishEntryMsg:
		f.entry = msg.entry
	case finishSequelsMsg:
		f.sequels, f.err, f.loaded = msg.sequels, msg.err, true
	case finishScoredMsg:
		if msg.err != nil {
			return toast("Couldn't save the score: "+firstLine(msg.err.Error()), true)
		}
		return toast(msg.note, false)
	case tea.KeyPressMsg:
		if f.rating {
			k := msg.String()
			switch {
			case key.Matches(msg, keySkipRate):
				f.rating = false
			case len(k) == 1 && k[0] >= '0' && k[0] <= '9':
				f.rating = false
				score, _ := strconv.Atoi(k)
				if score == 0 {
					score = 10
				}
				ctx, svc, id := p.ctx, p.svc, p.req.Media.ID
				return func() tea.Msg {
					note, err := svc.SetScore(ctx, id, float64(score))
					return finishScoredMsg{note, err}
				}
			}
			return nil
		}
		if len(f.sequels) == 0 {
			return nil
		}
		next := f.sequels[f.cursor]
		switch {
		case key.Matches(msg, keyWatchNext):
			return watch(session.Request{Media: next, Mode: p.req.Mode})
		case key.Matches(msg, keyPlanNext):
			ctx, svc := p.ctx, p.svc
			return func() tea.Msg {
				note, err := svc.SetListStatus(ctx, next.ID, tracker.Planning)
				if err != nil {
					return toastMsg{"Couldn't add to Planning: " + firstLine(err.Error()), true}
				}
				return toastMsg{next.DisplayTitle() + ": " + note, false}
			}
		case key.Matches(msg, keyNextInfo):
			return tea.Sequence(pop, push(newDetails(p.ctx, p.svc, next, p.req.Mode)))
		case msg.String() == "up" || msg.String() == "k":
			f.cursor = max(f.cursor-1, 0)
		case msg.String() == "down" || msg.String() == "j":
			f.cursor = min(f.cursor+1, len(f.sequels)-1)
		}
	}
	return nil
}

func (f *finishPanel) view(width int) string {
	var b strings.Builder
	m := f.p.req.Media
	b.WriteString("\n" + styleGood.Render(truncate("🎉 You finished "+m.DisplayTitle(), width)) + "\n\n")

	if f.rating {
		b.WriteString(styleTitle.Render("Rate it") + "\n")
		b.WriteString("Press " + styleKey.Render("1") + "–" + styleKey.Render("9") + ", or " + styleKey.Render("0") + " for 10 · " + styleKey.Render("s") + " to skip")
		if f.entry != nil && f.entry.Score > 0 {
			b.WriteString(styleMuted.Render(fmt.Sprintf("  (currently %s/10)", strconv.FormatFloat(f.entry.Score, 'f', -1, 64))))
		}
		return b.String() + "\n"
	}

	b.WriteString(styleTitle.Render("Up next") + "\n")
	switch {
	case !f.loaded:
		b.WriteString(styleMuted.Render("Looking for a sequel…"))
	case f.err != nil:
		b.WriteString(styleBad.Render("Couldn't look up sequels: " + firstLine(f.err.Error())))
	case len(f.sequels) == 0:
		b.WriteString(styleMuted.Render("No sequel on AniList yet. Press esc when you're done."))
	default:
		for i, s := range f.sequels {
			marker, style := "  ", styleTitle
			if i == f.cursor {
				marker, style = styleSelected.Render("▌ "), styleSelected
			}
			b.WriteString(marker + style.Render(truncate(s.DisplayTitle(), width-4)) + "\n")
			b.WriteString("  " + styleMuted.Render(truncate(mediaMeta(s), width-4)) + "\n")
		}
	}
	return b.String()
}
