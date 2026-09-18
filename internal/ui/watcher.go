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
	"github.com/RobertsMattL/chainsaw/internal/history"
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
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
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

	viewport viewport.Model
	input    textinput.Model
	viewBuf  *strings.Builder

	ready     bool
	width     int
	height    int
	filtering bool
	filter    string // active filter
	filterRe  *regexp.Regexp
	filterErr bool // filter looks like regex but has a syntax error
	atBottom  bool
	done      bool

	history     *history.History
	histMatches []string // fuzzy-filtered history entries
	histCursor  int      // -1 = typing; 0+ = index into histMatches (0=most recent)
	typedFilter string   // saved input before navigating history
}

func newWatchModel(cfg WatchConfig) watchModel {
	ti := textinput.New()
	ti.Placeholder = "filter or /regex/…"
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	ti.Prompt = "/ "
	h := history.Load()
	return watchModel{
		cfg:        cfg,
		input:      ti,
		atBottom:   true,
		viewBuf:    &strings.Builder{},
		history:    h,
		histCursor: -1,
	}
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
			m.refilter()
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
		m.typedFilter = m.filter
		m.histCursor = -1
		m.updateHistMatches()
		m.rebuildViewport()
	case "esc":
		if m.filter != "" {
			m.setFilter("")
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
		if m.filter != "" {
			m.history.Add(m.filter)
		}
		m.histCursor = -1
		m.rebuildViewport()

	case "esc":
		m.filtering = false
		m.input.Blur()
		m.histCursor = -1
		m.setFilter("")
		m.input.SetValue("")
		m.refilter()
		m.rebuildViewport()

	case "ctrl+c":
		return m, tea.Quit

	case "up":
		if len(m.histMatches) == 0 {
			break
		}
		if m.histCursor == -1 {
			m.typedFilter = m.input.Value()
		}
		if m.histCursor < len(m.histMatches)-1 {
			m.histCursor++
		}
		sel := m.histMatches[m.histCursor]
		m.input.SetValue(sel)
		m.input.CursorEnd()
		m.setFilter(sel)
		m.refilter()
		m.rebuildViewport()

	case "down":
		if m.histCursor < 0 {
			break
		}
		m.histCursor--
		if m.histCursor < 0 {
			m.input.SetValue(m.typedFilter)
			m.input.CursorEnd()
			m.setFilter(m.typedFilter)
		} else {
			sel := m.histMatches[m.histCursor]
			m.input.SetValue(sel)
			m.input.CursorEnd()
			m.setFilter(sel)
		}
		m.refilter()
		m.rebuildViewport()

	default:
		var inputCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		cmds = append(cmds, inputCmd)
		if v := m.input.Value(); v != m.filter {
			m.histCursor = -1
			m.typedFilter = v
			m.setFilter(v)
			m.updateHistMatches()
			m.refilter()
			m.rebuildViewport()
		}
	}
	return m, tea.Batch(cmds...)
}

// — filter helpers —

func (m *watchModel) setFilter(f string) {
	m.filter = f
	m.filterRe = nil
	m.filterErr = false
	if f != "" && containsRegexMeta(f) {
		if re, err := regexp.Compile(f); err == nil {
			m.filterRe = re
		} else {
			m.filterErr = true
		}
	}
}

func containsRegexMeta(s string) bool {
	return strings.ContainsAny(s, `[\](){}^$.|*+?`)
}

func (m *watchModel) matches(raw string) bool {
	if m.filter == "" {
		return true
	}
	if m.filterRe != nil {
		return m.filterRe.MatchString(raw)
	}
	return strings.Contains(strings.ToLower(raw), strings.ToLower(m.filter))
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

// — history helpers —

func (m *watchModel) updateHistMatches() {
	if m.history == nil {
		m.histMatches = nil
		return
	}
	needle := strings.ToLower(m.typedFilter)
	if needle == "" {
		m.histMatches = make([]string, len(m.history.Entries))
		copy(m.histMatches, m.history.Entries)
		return
	}
	m.histMatches = m.histMatches[:0]
	for _, e := range m.history.Entries {
		if fuzzyMatch(needle, strings.ToLower(e)) {
			m.histMatches = append(m.histMatches, e)
		}
	}
}

// fuzzyMatch returns true if all runes of needle appear in haystack in order.
func fuzzyMatch(needle, haystack string) bool {
	hi := 0
	hay := []rune(haystack)
	for _, ch := range needle {
		found := false
		for ; hi < len(hay); hi++ {
			if hay[hi] == ch {
				hi++
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (m watchModel) histDisplayCount() int {
	if !m.filtering {
		return 0
	}
	n := len(m.histMatches)
	if n > 5 {
		n = 5
	}
	return n
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
	m.viewport.Height = m.vpHeight()
	m.viewport.SetContent(m.viewBuf.String())
	if m.atBottom {
		m.viewport.GotoBottom()
	}
}

func (m watchModel) vpHeight() int {
	// header: 2 lines, footer separator+input: 2 lines, history items
	h := m.height - 4 - m.histDisplayCount()
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
	watchFilterReBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	watchFilterErrBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	watchDoneStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	histEntryStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	histSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	histHintStyle     = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("240"))
)

func (m watchModel) headerView() string {
	tag := watchTagStyle.Render(m.cfg.LogConfig.Name)
	title := watchTitleStyle.Render("⛓") + "  " + tag

	if m.filter != "" {
		var badge string
		if m.filterErr {
			badge = watchFilterErrBadge.Render(" ~[!re] " + m.filter + "~")
		} else if m.filterRe != nil {
			badge = watchFilterReBadge.Render(" ~[re] " + m.filter + "~")
		} else {
			badge = watchFilterBadge.Render(" ~" + m.filter + "~")
		}
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
		var sb strings.Builder

		// History items: render in reverse so most recent (index 0) is closest to input.
		shown := m.histDisplayCount()
		for i := shown - 1; i >= 0; i-- {
			e := m.histMatches[i]
			display := truncate(e, m.width-4)
			if i == m.histCursor {
				sb.WriteString(histSelectedStyle.Render(" > " + display))
			} else {
				sb.WriteString(histEntryStyle.Render("   " + display))
			}
			sb.WriteByte('\n')
		}

		sb.WriteString(m.input.View())
		if m.filterErr {
			sb.WriteString("  " + watchFilterErrBadge.Render("[invalid regex]"))
		} else if m.filterRe != nil {
			sb.WriteString("  " + watchFilterReBadge.Render("[regex]"))
		} else if len(m.history.Entries) == 0 {
			sb.WriteString("  " + histHintStyle.Render("↑ history (Enter to save)"))
		} else if len(m.histMatches) == 0 && m.typedFilter != "" {
			sb.WriteString("  " + histHintStyle.Render("no history matches"))
		}
		bar = sb.String()
	} else {
		var parts []string
		if m.filter != "" {
			filterDisplay := watchFilterBadge.Render("/ ") + watchFilterBadge.Render(truncate(m.filter, 40))
			if m.filterRe != nil {
				filterDisplay = watchFilterReBadge.Render("/ ") + watchFilterReBadge.Render(truncate(m.filter, 40))
			} else if m.filterErr {
				filterDisplay = watchFilterErrBadge.Render("/ ") + watchFilterErrBadge.Render(truncate(m.filter, 40))
			}
			parts = append(parts, filterDisplay, watchHelpStyle.Render("esc=clear  /=edit"))
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

func truncate(s string, max int) string {
	if max < 4 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
