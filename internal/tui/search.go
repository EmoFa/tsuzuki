package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/domain"
)

const (
	searchDebounce = 350 * time.Millisecond
	searchMinChars = 2
	searchResults  = 25
)

type searchDebounceMsg struct {
	seq   int
	query string
}

type searchResultMsg struct {
	seq     int
	results []anilist.Media
	err     error
}

type searchScreen struct {
	ctx     context.Context
	svc     Services
	input   textinput.Model
	seq     int
	loading bool
	err     error
	query   string // the query results belong to
	results []anilist.Media
	list    list
}

func newSearch(ctx context.Context, svc Services) *searchScreen {
	in := textinput.New()
	in.Placeholder = "Search anime…"
	in.Prompt = "/ "
	in.CharLimit = 100
	return &searchScreen{ctx: ctx, svc: svc, input: in}
}

func (s *searchScreen) Init() tea.Cmd { return s.input.Focus() }
func (s *searchScreen) Title() string { return "Search" }

// CapturesInput: while typing, q and ? are text.
func (s *searchScreen) CapturesInput() bool { return s.input.Focused() }

var (
	keyOpen       = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
	keyToResults  = key.NewBinding(key.WithKeys("down", "enter"), key.WithHelp("↓/enter", "results"))
	keyEditQuery  = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "edit search"))
	keyLeaveInput = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
)

func (s *searchScreen) Help() []key.Binding {
	if s.input.Focused() {
		return []key.Binding{keyToResults, keyLeaveInput}
	}
	return []key.Binding{keyOpen, keyEditQuery}
}

func (s *searchScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case searchDebounceMsg:
		if msg.seq == s.seq {
			return s, s.search(msg.query)
		}
		return s, nil

	case searchResultMsg:
		if msg.seq != s.seq {
			return s, nil
		}
		s.loading, s.err = false, msg.err
		if msg.err == nil {
			s.results = msg.results
			s.list.setLen(len(s.results))
			s.list.setCursor(0)
		}
		return s, nil

	case tea.KeyPressMsg:
		if s.input.Focused() {
			switch msg.String() {
			case "esc":
				return s, pop
			case "down", "enter":
				if msg.String() == "enter" && strings.TrimSpace(s.input.Value()) != s.query {
					// Search now instead of waiting for the debounce.
					s.seq++
					return s, s.search(s.input.Value())
				}
				if len(s.results) > 0 {
					s.input.Blur()
				}
				return s, nil
			}
			before := s.input.Value()
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			if s.input.Value() != before {
				s.seq++
				seq, q := s.seq, s.input.Value()
				return s, tea.Batch(cmd, tea.Tick(searchDebounce, func(time.Time) tea.Msg { return searchDebounceMsg{seq, q} }))
			}
			return s, cmd
		}

		switch {
		case key.Matches(msg, keyEditQuery):
			return s, s.input.Focus()
		case key.Matches(msg, keyOpen):
			if len(s.results) > 0 {
				return s, push(newDetails(s.ctx, s.svc, s.results[s.list.cursor], ""))
			}
		case msg.String() == "up" && s.list.cursor == 0:
			return s, s.input.Focus()
		default:
			s.list.handleKey(msg, 5)
		}
	}
	return s, nil
}

func (s *searchScreen) search(query string) tea.Cmd {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < searchMinChars {
		s.loading = false
		return nil
	}
	s.loading, s.query = true, query
	seq, ctx, svc := s.seq, s.ctx, s.svc
	return func() tea.Msg {
		res, err := svc.Search(ctx, query)
		if len(res) > searchResults {
			res = res[:searchResults]
		}
		return searchResultMsg{seq: seq, results: res, err: err}
	}
}

func (s *searchScreen) View(width, height int) string {
	var b strings.Builder
	s.input.SetWidth(max(width-4, 10))
	b.WriteString("\n" + s.input.View() + "\n\n")

	switch {
	case s.loading:
		b.WriteString(styleMuted.Render("Searching AniList…"))
		return b.String()
	case s.err != nil:
		b.WriteString(styleBad.Render("Search failed: " + s.err.Error()))
		return b.String()
	case s.query != "" && len(s.results) == 0:
		b.WriteString(styleMuted.Render("No results for “" + s.query + "”."))
		return b.String()
	}

	rows := max((height-4)/2, 1)
	start, end := s.list.window(rows)
	for i := start; i < end; i++ {
		m := s.results[i]
		marker, titleStyle := "  ", styleTitle
		if i == s.list.cursor && !s.input.Focused() {
			marker, titleStyle = styleSelected.Render("▌ "), styleSelected
		}
		b.WriteString(marker + titleStyle.Render(truncate(m.DisplayTitle(), width-4)) + "\n")
		b.WriteString("  " + styleMuted.Render(truncate(mediaMeta(m), width-4)) + "\n")
	}
	return b.String()
}

// modeOrDefault is used when a screen was opened without a known mode.
func modeOrDefault(m domain.Mode, svc Services) domain.Mode {
	if m != "" {
		return m
	}
	return domain.Mode(svc.Settings().Config.General.Mode)
}
