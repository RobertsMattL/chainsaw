package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/RobertsMattL/chainsaw/internal/config"
	"github.com/RobertsMattL/chainsaw/internal/highlight"
	"github.com/RobertsMattL/chainsaw/internal/source"
)

const maxStoredLines = 50_000

// WatchConfig bundles everything RunWatcher needs.
type WatchConfig struct {
	LogConfig  *config.LogConfig
	Engine     *highlight.Engine
	IncludeRe  *regexp.Regexp
	ExcludeRe  *regexp.Regexp
	ShowLabels bool
	Lines      <-chan source.Line
}

// RunWatcher starts the interactive TUI watcher.
func RunWatcher(cfg WatchConfig) error {
	m := newWatchModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// — stored line —

type storedLine struct {
	raw    string
	styled string
	label  string
}

// — tea messages —

type lineMsg source.Line
type doneMsg struct{}

func listenForLine(ch <-chan source.Line) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return lineMsg(line)
	}
}

// — model —

type watchModel struct {
	cfg      WatchConfig
	lines    []storedLine
	filtered []int // indices into lines that match the active filter

	viewport  viewport.Model
	input     textinput.Model
	viewBuf   *strings.Builder // pointer avoids copy-by-value panic

	ready     bool
	width     int
	height    int
	filtering bool   // filter bar is open
	filter    string // active filter (live as user types)
	atBottom  bool
	done      bool
}

func newWatchModel(cfg WatchConfig) watchModel {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	ti.Prompt = "/ "
	return watchModel{cfg: cfg, input: ti, atBottom: true, viewBuf: &strings.Builder{}}
}

func (m watchModel) Init() tea.Cmd {
	return listenForLine(m.cfg.Lines)
}

// — update —

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.viewport = viewport.New(msg.Width, m.vpHeight())
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = m.vpHeight()
		}
		m.rebuildViewport()
		return m, nil

	case lineMsg:
		cmds = append(cmds, listenForLine(m.cfg.Lines))
		sl := source.Line(msg)

		if m.cfg.IncludeRe != nil && !m.cfg.IncludeRe.MatchString(sl.Content) {
			return m, tea.Batch(cmds...)
		}
		if m.cfg.ExcludeRe != nil && m.cfg.ExcludeRe.MatchString(sl.Content) {
			return m, tea.Batch(cmds...)
		}

		ll := storedLine{
			raw:    sl.Content,
			styled: m.cfg.Engine.Apply(sl.Content),
			label:  sl.Label,
		}

		needFullRebuild := len(m.lines) >= maxStoredLines
		if needFullRebuild {
			m.lines = m.lines[1:]
			m.refilter() // rebuilds filtered + viewBuf after drop
		}

		m.lines = append(m.lines, ll)
		if m.matches(ll.raw) {
			m.filtered = append(m.filtered, len(m.lines)-1)
			m.writeLine(ll)
			m.rebuildViewport()
		} else if needFullRebuild {
			m.rebuildViewport()
		}
		return m, tea.Batch(cmds...)

	case doneMsg:
		m.done = true
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			return m.updateFiltering(msg, cmds)
		}
		return m.updateNormal(msg, cmds)
	}

	if m.ready && !m.filtering {
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmds = append(cmds, vpCmd)
		m.atBottom = m.viewport.AtBottom()
	}
	return m, tea.Batch(cmds...)
}

func (m watchModel) updateNormal(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "/":
		m.filtering = true
		m.input.SetValue(m.filter)
		m.input.CursorEnd()
		m.input.Focus()
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.input.SetValue("")
			m.refilter()
			m.rebuildViewport()
		}
	case "g":
		m.viewport.GotoTop()
		m.atBottom = false
	case "G":
		m.viewport.GotoBottom()
		m.atBottom = true
	case "q", "ctrl+c":
		return m, tea.Quit
	default:
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmds = append(cmds, vpCmd)
		m.atBottom = m.viewport.AtBottom()
	}
	return m, tea.Batch(cmds...)
}

func (m watchModel) updateFiltering(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		m.input.Blur()
		// filter already up to date from live typing; commit it
	case "esc":
		m.filtering = false
		m.input.Blur()
		// esc: clear the filter
		m.filter = ""
		m.input.SetValue("")
		m.refilter()
		m.rebuildViewport()
	case "ctrl+c":
		return m, tea.Quit
	default:
		var inputCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		cmds = append(cmds, inputCmd)
		if v := m.input.Value(); v != m.filter {
			m.filter = v
			m.refilter()
			m.rebuildViewport()
		}
	}
	return m, tea.Batch(cmds...)
}

// — filter helpers —

func (m *watchModel) matches(raw string) bool {
	return m.filter == "" ||
		strings.Contains(strings.ToLower(raw), strings.ToLower(m.filter))
}

func (m *watchModel) refilter() {
	m.filtered = m.filtered[:0]
	m.viewBuf.Reset()
	for i, ll := range m.lines {
		if m.matches(ll.raw) {
			m.filtered = append(m.filtered, i)
			m.writeLine(ll)
		}
	}
}

// — viewport helpers —

func (m *watchModel) writeLine(ll storedLine) {
	if m.cfg.ShowLabels && ll.label != "" {
		m.viewBuf.WriteString(
			lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Faint(true).
				Render("["+ll.label+"] "),
		)
	}
	m.viewBuf.WriteString(ll.styled)
	m.viewBuf.WriteByte('\n')
}

func (m *watchModel) rebuildViewport() {
	if !m.ready {
		return
	}
	m.viewport.SetContent(m.viewBuf.String())
	if m.atBottom {
		m.viewport.GotoBottom()
	}
}

func (m watchModel) vpHeight() int {
	// header: 2 lines, footer: 2 lines
	h := m.height - 4
	if h < 1 {
		return 1
	}
	return h
}

// — view —

func (m watchModel) View() string {
	if !m.ready {
		return ""
	}
	return m.headerView() + m.viewport.View() + m.footerView()
}

var (
	watchTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	watchTagStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Background(lipgloss.Color("236")).Padding(0, 1)
	watchCountStyle  = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("244"))
	watchSepStyle    = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("237"))
	watchHelpStyle   = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("244"))
	watchFilterBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	watchDoneStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

func (m watchModel) headerView() string {
	tag := watchTagStyle.Render(m.cfg.LogConfig.Name)
	title := watchTitleStyle.Render("⛓") + "  " + tag

	if m.filter != "" {
		badge := watchFilterBadge.Render(" ~" + m.filter + "~")
		title += badge
	}

	count := watchCountStyle.Render(fmt.Sprintf("%d/%d", len(m.filtered), len(m.lines)))
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(count)
	if gap < 1 {
		gap = 1
	}
	top := title + strings.Repeat(" ", gap) + count
	sep := watchSepStyle.Render(strings.Repeat("─", m.width))
	return top + "\n" + sep + "\n"
}

func (m watchModel) footerView() string {
	sep := watchSepStyle.Render(strings.Repeat("─", m.width))

	var bar string
	if m.filtering {
		bar = m.input.View()
	} else {
		var parts []string
		if m.filter != "" {
			parts = append(parts,
				watchHelpStyle.Render("/=edit"),
				watchHelpStyle.Render("esc=clear"),
			)
		} else {
			parts = append(parts, watchHelpStyle.Render("/=filter"))
		}
		parts = append(parts, watchHelpStyle.Render("↑↓ scroll  G=bottom  g=top  q=quit"))
		if !m.atBottom {
			parts = append(parts, watchFilterBadge.Render("↓ more"))
		}
		if m.done {
			parts = append(parts, watchDoneStyle.Render("[stream ended]"))
		}
		bar = strings.Join(parts, watchHelpStyle.Render("  ·  "))
	}

	return "\n" + sep + "\n" + bar
}
