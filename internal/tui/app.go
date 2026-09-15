package tui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/EmoFa/anitui/internal/session"
)

const (
	toastDuration  = 4 * time.Second
	noticeDuration = 15 * time.Second
)

// screen is one page of the UI. Screens live on a stack; the top one gets input.
type screen interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (screen, tea.Cmd)
	View(width, height int) string
	Title() string
	Help() []key.Binding
}

// inputCapturer screens (e.g. a focused text field) receive q, ? and esc as
// ordinary keys.
type inputCapturer interface{ CapturesInput() bool }

// refresher screens reload their data when they become the top screen again.
type refresher interface{ Refresh() tea.Cmd }

// backHandler screens decide what leaving them means (e.g. stop playback).
type backHandler interface{ Back() tea.Cmd }

// Messages screens use to drive navigation and playback.
type (
	pushMsg  struct{ s screen }
	popMsg   struct{}
	toastMsg struct {
		text string
		err  bool
	}
	toastExpiredMsg struct{ id int }
	watchMsg        struct{ req session.Request }
	stopWatchMsg    struct{}
	statusMsg       struct {
		gen int
		st  session.Status
	}
	watchDoneMsg struct {
		gen int
		err error
	}
)

// NoticeMsg shows a message that needs the user's attention, such as a
// browser verification prompt. Safe to send from any goroutine.
type NoticeMsg struct{ Text string }

func push(s screen) tea.Cmd { return func() tea.Msg { return pushMsg{s} } }
func pop() tea.Msg          { return popMsg{} }
func toast(text string, isErr bool) tea.Cmd {
	return func() tea.Msg { return toastMsg{text, isErr} }
}
func watch(req session.Request) tea.Cmd { return func() tea.Msg { return watchMsg{req} } }

var (
	keyQuit = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "back/quit"))
	keyHelp = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help"))
	keyBack = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
)

// Model is the root of the UI.
type Model struct {
	svc  Services
	ctx  context.Context
	send func(tea.Msg)

	stack         []screen
	width, height int
	help          bool

	toast    string
	toastErr bool
	toastID  int

	gen      int // identifies the current watch; stale messages are dropped
	watching bool
	cancel   context.CancelFunc
	pending  *session.Request // started once the current watch has stopped
	// watchRunning/watchStopped let Run wait for the last watch to save progress.
	watchRunning *atomic.Bool
	watchStopped chan struct{}
}

func New(ctx context.Context, svc Services) *Model {
	m := &Model{svc: svc, ctx: ctx}
	m.stack = []screen{newHome(ctx, svc)}
	return m
}

// Run starts the UI and blocks until the user quits.
func Run(ctx context.Context, svc Services, notices <-chan string) error {
	m := New(ctx, svc)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	m.send = p.Send
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case text := <-notices:
				p.Send(NoticeMsg{text})
			case <-done:
				return
			}
		}
	}()
	_, err := p.Run()
	m.stopWatch()
	if m.watchRunning != nil && m.watchRunning.Load() {
		select {
		case <-m.watchStopped:
		case <-time.After(10 * time.Second):
		}
	}
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

func (m *Model) Init() tea.Cmd { return m.top().Init() }

func (m *Model) top() screen { return m.stack[len(m.stack)-1] }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.stopWatch()
			return m, tea.Quit
		}
		if m.help {
			m.help = false
			return m, nil
		}
		if c, ok := m.top().(inputCapturer); !ok || !c.CapturesInput() {
			switch msg.String() {
			case "?":
				m.help = true
				return m, nil
			case "q", "esc":
				if len(m.stack) == 1 {
					if msg.String() == "q" {
						m.stopWatch()
						return m, tea.Quit
					}
					return m, nil
				}
				if b, ok := m.top().(backHandler); ok {
					return m, b.Back()
				}
				return m, pop
			}
		}

	case pushMsg:
		m.stack = append(m.stack, msg.s)
		return m, msg.s.Init()

	case popMsg:
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
		if r, ok := m.top().(refresher); ok {
			return m, r.Refresh()
		}
		return m, nil

	case toastMsg:
		return m, m.showToast(msg.text, msg.err, toastDuration)

	case NoticeMsg:
		return m, m.showToast(msg.Text, false, noticeDuration)

	case toastExpiredMsg:
		if msg.id == m.toastID {
			m.toast = ""
		}
		return m, nil

	case watchMsg:
		return m, m.startWatch(msg.req)

	case stopWatchMsg:
		m.pending = nil
		m.stopWatch()
		return m, nil

	case statusMsg:
		if msg.gen != m.gen {
			return m, nil
		}

	case watchDoneMsg:
		return m, m.watchDone(msg)
	}

	s, cmd := m.top().Update(msg)
	m.stack[len(m.stack)-1] = s
	return m, cmd
}

func (m *Model) showToast(text string, isErr bool, d time.Duration) tea.Cmd {
	m.toastID++
	id := m.toastID
	m.toast, m.toastErr = text, isErr
	return tea.Tick(d, func(time.Time) tea.Msg { return toastExpiredMsg{id} })
}

// startWatch plays req. If something is already playing it is stopped first
// and req starts once it has finished saving its progress.
func (m *Model) startWatch(req session.Request) tea.Cmd {
	if m.watching {
		m.pending = &req
		m.stopWatch()
		return nil
	}
	m.gen++
	gen := m.gen
	ctx, cancel := context.WithCancel(m.ctx)
	m.watching, m.cancel = true, cancel

	screenCmd := m.showPlaying(req)
	send := m.send
	running, stopped := &atomic.Bool{}, make(chan struct{})
	m.watchRunning, m.watchStopped = running, stopped
	run := func() tea.Msg {
		running.Store(true)
		defer close(stopped)
		err := m.svc.Watch(ctx, req, func(st session.Status) {
			if send != nil {
				send(statusMsg{gen, st})
			}
		})
		return watchDoneMsg{gen, err}
	}
	return tea.Batch(screenCmd, run)
}

// showPlaying puts a fresh now-playing screen on top, replacing an existing one.
func (m *Model) showPlaying(req session.Request) tea.Cmd {
	p := newPlaying(req)
	if _, ok := m.top().(*playingScreen); ok {
		m.stack[len(m.stack)-1] = p
	} else {
		m.stack = append(m.stack, p)
	}
	return p.Init()
}

func (m *Model) stopWatch() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Model) watchDone(msg watchDoneMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil
	}
	m.watching, m.cancel = false, nil
	if m.pending != nil {
		req := *m.pending
		m.pending = nil
		return m.startWatch(req)
	}
	var cmds []tea.Cmd
	if _, ok := m.top().(*playingScreen); ok {
		cmds = append(cmds, pop)
	}
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		cmds = append(cmds, toast(firstLine(msg.err.Error()), true))
	}
	return tea.Batch(cmds...)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "anitui"
	return v
}

func (m *Model) render() string {
	if m.width == 0 {
		return ""
	}
	header := m.header()
	footer := m.footer()
	bodyHeight := max(m.height-lipgloss.Height(header)-lipgloss.Height(footer), 1)

	var body string
	if m.help {
		body = lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, m.helpView())
	} else {
		body = lipgloss.NewStyle().Width(m.width).Height(bodyHeight).MaxHeight(bodyHeight).
			Render(m.top().View(m.width, bodyHeight))
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *Model) header() string {
	var crumbs []string
	for _, s := range m.stack {
		crumbs = append(crumbs, s.Title())
	}
	return styleBrand.Render("anitui") + styleMuted.Render("  ›  ") + truncate(strings.Join(crumbs, " › "), m.width-12)
}

func (m *Model) footer() string {
	var lines []string
	if m.toast != "" {
		style := styleInfo
		if m.toastErr {
			style = styleBad
		}
		lines = append(lines, style.Render(truncate(m.toast, m.width)))
	}
	var parts []string
	seen := map[string]bool{}
	for _, b := range append(m.top().Help(), m.globalKeys()...) {
		if seen[b.Help().Key] {
			continue
		}
		seen[b.Help().Key] = true
		parts = append(parts, styleKey.Render(b.Help().Key)+" "+styleMuted.Render(b.Help().Desc))
	}
	lines = append(lines, truncate(strings.Join(parts, styleMuted.Render(" · ")), m.width))
	return strings.Join(lines, "\n")
}

func (m *Model) globalKeys() []key.Binding {
	if len(m.stack) == 1 {
		return []key.Binding{keyHelp, key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))}
	}
	return []key.Binding{keyHelp, keyBack}
}

func (m *Model) helpView() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(m.top().Title()+" keys") + "\n\n")
	for _, k := range append(m.top().Help(), keyHelp, keyBack, keyQuit,
		key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit anitui"))) {
		b.WriteString(styleKey.Render(padRight(k.Help().Key, 10)) + " " + k.Help().Desc + "\n")
	}
	b.WriteString("\n" + styleMuted.Render("press any key to close"))
	return styleHelpBox.Render(b.String())
}

func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
