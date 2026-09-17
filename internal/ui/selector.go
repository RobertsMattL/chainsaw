package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/somewearlabs/chainsaw/internal/config"
)

// SelectConfig runs an interactive TUI to pick a log config.
// Returns the selected config, or nil if the user cancelled.
func SelectConfig(configs []config.LogConfig) (*config.LogConfig, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("no configurations saved — run 'chainsaw config add' first")
	}

	items := make([]list.Item, len(configs))
	for i, c := range configs {
		c := c
		items[i] = configItem{cfg: &c}
	}

	const listHeight = 16
	delegate := configDelegate{}
	l := list.New(items, delegate, 60, listHeight)
	l.Title = "Select a log configuration"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = titleStyle
	l.Styles.FilterPrompt = filterPromptStyle
	l.Styles.FilterCursor = filterCursorStyle

	m := model{list: l}
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return nil, err
	}
	final := result.(model)
	if final.quitting || final.selected == nil {
		return nil, nil
	}
	return final.selected, nil
}

// — styles —

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")).
			MarginLeft(2)

	filterPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("86"))

	filterCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("212"))

	selectedNameStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86"))

	normalNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	descStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	sourceCountStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Faint(true)

	selectedBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("212"))

	normalBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("237"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			MarginLeft(2)
)

// — list item —

type configItem struct {
	cfg *config.LogConfig
}

func (i configItem) FilterValue() string { return i.cfg.Name + " " + i.cfg.Description }
func (i configItem) Title() string       { return i.cfg.Name }
func (i configItem) Description() string { return i.cfg.Description }

// — delegate —

type configDelegate struct{}

func (d configDelegate) Height() int                             { return 2 }
func (d configDelegate) Spacing() int                           { return 1 }
func (d configDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d configDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ci, ok := item.(configItem)
	if !ok {
		return
	}

	selected := index == m.Index()

	bar := "  "
	var nameStr, descStr, countStr string
	if selected {
		bar = selectedBarStyle.Render("▌ ")
		nameStr = selectedNameStyle.Render(ci.cfg.Name)
	} else {
		bar = normalBarStyle.Render("  ")
		nameStr = normalNameStyle.Render(ci.cfg.Name)
	}

	desc := ci.cfg.Description
	if desc == "" {
		desc = "no description"
	}
	descStr = descStyle.Render(desc)

	srcs := fmt.Sprintf("%d source", len(ci.cfg.Sources))
	if len(ci.cfg.Sources) != 1 {
		srcs += "s"
	}
	if ci.cfg.JSONMode {
		srcs += " · json"
	}
	countStr = sourceCountStyle.Render(srcs)

	fmt.Fprintf(w, "%s%s  %s\n%s%s",
		bar, nameStr, countStr,
		"   ", descStr,
	)
}

// — model —

type model struct {
	list     list.Model
	selected *config.LogConfig
	quitting bool
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "enter":
			item, ok := m.list.SelectedItem().(configItem)
			if ok {
				m.selected = item.cfg
			}
			return m, tea.Quit
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(m.list.View())
	sb.WriteString("\n\n")
	sb.WriteString(helpStyle.Render("enter: select  /: filter  ↑↓: navigate  q: quit"))
	sb.WriteString("\n")
	return sb.String()
}
