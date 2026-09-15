package tui

import tea "charm.land/bubbletea/v2"

// list tracks a cursor over n rows and the scroll offset needed to show it.
type list struct {
	n      int
	cursor int
	offset int
}

func (l *list) setLen(n int) {
	l.n = n
	l.clamp()
}

func (l *list) setCursor(i int) {
	l.cursor = i
	l.clamp()
}

func (l *list) move(delta int) {
	l.cursor += delta
	l.clamp()
}

func (l *list) clamp() {
	if l.cursor >= l.n {
		l.cursor = l.n - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// handleKey applies navigation keys, reporting whether the key was one.
func (l *list) handleKey(msg tea.KeyPressMsg, page int) bool {
	switch msg.String() {
	case "up", "k":
		l.move(-1)
	case "down", "j":
		l.move(1)
	case "pgup", "ctrl+u":
		l.move(-max(page, 1))
	case "pgdown", "ctrl+d":
		l.move(max(page, 1))
	case "home", "g":
		l.setCursor(0)
	case "end", "G":
		l.setCursor(l.n - 1)
	default:
		return false
	}
	return true
}

// window returns the rows [start, end) to draw in height lines, scrolling so
// the cursor stays visible.
func (l *list) window(height int) (start, end int) {
	if height <= 0 || l.n == 0 {
		return 0, 0
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+height {
		l.offset = l.cursor - height + 1
	}
	if l.offset > max(l.n-height, 0) {
		l.offset = max(l.n-height, 0)
	}
	return l.offset, min(l.offset+height, l.n)
}
