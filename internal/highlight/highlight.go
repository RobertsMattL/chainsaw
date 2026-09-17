package highlight

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/RobertsMattL/chainsaw/internal/config"
)

type Engine struct {
	rules    []compiledRule
	jsonMode bool
}

type compiledRule struct {
	re    *regexp.Regexp
	style lipgloss.Style
	mode  string // "line" or "match"
}

var namedColors = map[string]lipgloss.Color{
	"black":   lipgloss.Color("0"),
	"red":     lipgloss.Color("1"),
	"green":   lipgloss.Color("2"),
	"yellow":  lipgloss.Color("3"),
	"blue":    lipgloss.Color("4"),
	"magenta": lipgloss.Color("5"),
	"cyan":    lipgloss.Color("6"),
	"white":   lipgloss.Color("7"),
	"gray":    lipgloss.Color("8"),
	// Bright variants
	"bright-red":     lipgloss.Color("9"),
	"bright-green":   lipgloss.Color("10"),
	"bright-yellow":  lipgloss.Color("11"),
	"bright-blue":    lipgloss.Color("12"),
	"bright-magenta": lipgloss.Color("13"),
	"bright-cyan":    lipgloss.Color("14"),
	"bright-white":   lipgloss.Color("15"),
	// Tasteful 256-color extras
	"orange":     lipgloss.Color("208"),
	"pink":       lipgloss.Color("212"),
	"lime":       lipgloss.Color("154"),
	"teal":       lipgloss.Color("86"),
	"lavender":   lipgloss.Color("183"),
	"gold":       lipgloss.Color("220"),
	"salmon":     lipgloss.Color("210"),
	"slate":      lipgloss.Color("244"),
	"dim-red":    lipgloss.Color("124"),
	"dim-green":  lipgloss.Color("28"),
	"dim-yellow": lipgloss.Color("136"),
}

func parseStyle(s string) lipgloss.Style {
	style := lipgloss.NewStyle()
	parts := strings.Split(strings.ToLower(s), "+")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch p {
		case "bold":
			style = style.Bold(true)
		case "italic":
			style = style.Italic(true)
		case "underline":
			style = style.Underline(true)
		case "faint":
			style = style.Faint(true)
		case "strikethrough":
			style = style.Strikethrough(true)
		case "reverse":
			style = style.Reverse(true)
		default:
			if c, ok := namedColors[p]; ok {
				style = style.Foreground(c)
			} else if strings.HasPrefix(p, "#") {
				style = style.Foreground(lipgloss.Color(p))
			}
		}
	}
	return style
}

func New(highlights []config.Highlight, jsonMode bool) (*Engine, error) {
	e := &Engine{jsonMode: jsonMode}
	for _, h := range highlights {
		re, err := regexp.Compile(h.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", h.Pattern, err)
		}
		mode := h.Mode
		if mode == "" {
			mode = "line"
		}
		e.rules = append(e.rules, compiledRule{
			re:    re,
			style: parseStyle(h.Style),
			mode:  mode,
		})
	}
	return e, nil
}

func (e *Engine) Apply(line string) string {
	if e.jsonMode {
		if rendered := tryRenderJSON(line); rendered != "" {
			return rendered
		}
	}

	for _, r := range e.rules {
		if r.re.MatchString(line) {
			if r.mode == "line" {
				return r.style.Render(line)
			}
			// match mode: highlight only the matched portions
			line = applyMatchHighlight(line, r.re, r.style)
		}
	}
	return line
}

func applyMatchHighlight(line string, re *regexp.Regexp, style lipgloss.Style) string {
	var sb strings.Builder
	last := 0
	for _, loc := range re.FindAllStringIndex(line, -1) {
		sb.WriteString(line[last:loc[0]])
		sb.WriteString(style.Render(line[loc[0]:loc[1]]))
		last = loc[1]
	}
	sb.WriteString(line[last:])
	return sb.String()
}

// JSON pretty-printing with syntax highlighting

var (
	jsonKeyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	jsonStrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("150"))
	jsonNumStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	jsonBoolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	jsonNullStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Faint(true)
	jsonBraceStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	jsonCommaStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	jsonLevelStyles = map[string]lipgloss.Style{
		"fatal":    lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true),
		"critical": lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true),
		"error":    lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		"err":      lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		"warn":     lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		"warning":  lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		"info":     lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		"debug":    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		"trace":    lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Faint(true),
	}
)

func tryRenderJSON(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return ""
	}
	return renderJSONObject(obj, 0)
}

func renderJSONObject(obj map[string]any, depth int) string {
	if len(obj) == 0 {
		return jsonBraceStyle.Render("{}")
	}

	indent := strings.Repeat("  ", depth)
	inner := strings.Repeat("  ", depth+1)

	// Prioritize common log fields for display order
	priority := []string{"time", "timestamp", "ts", "level", "msg", "message", "error", "err"}
	ordered := prioritizeKeys(obj, priority)

	var lines []string
	for _, k := range ordered {
		v := obj[k]
		keyStr := jsonKeyStyle.Render(`"` + k + `"`) + jsonCommaStyle.Render(": ")
		valStr := renderJSONValue(v, depth+1, k)
		lines = append(lines, inner+keyStr+valStr)
	}

	return jsonBraceStyle.Render("{") + "\n" +
		strings.Join(lines, jsonCommaStyle.Render(",")+"\n") + "\n" +
		indent + jsonBraceStyle.Render("}")
}

func renderJSONValue(v any, depth int, key string) string {
	switch val := v.(type) {
	case string:
		// Apply level styling for level/severity fields
		if key == "level" || key == "severity" || key == "lvl" {
			lower := strings.ToLower(val)
			if style, ok := jsonLevelStyles[lower]; ok {
				return style.Render(`"` + val + `"`)
			}
		}
		return jsonStrStyle.Render(`"` + val + `"`)
	case float64:
		if val == float64(int64(val)) {
			return jsonNumStyle.Render(fmt.Sprintf("%d", int64(val)))
		}
		return jsonNumStyle.Render(fmt.Sprintf("%g", val))
	case bool:
		if val {
			return jsonBoolStyle.Render("true")
		}
		return jsonBoolStyle.Render("false")
	case nil:
		return jsonNullStyle.Render("null")
	case map[string]any:
		return renderJSONObject(val, depth)
	case []any:
		return renderJSONArray(val, depth)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func renderJSONArray(arr []any, depth int) string {
	if len(arr) == 0 {
		return jsonBraceStyle.Render("[]")
	}
	indent := strings.Repeat("  ", depth)
	inner := strings.Repeat("  ", depth+1)
	var items []string
	for _, v := range arr {
		items = append(items, inner+renderJSONValue(v, depth+1, ""))
	}
	return jsonBraceStyle.Render("[") + "\n" +
		strings.Join(items, jsonCommaStyle.Render(",")+"\n") + "\n" +
		indent + jsonBraceStyle.Render("]")
}

func prioritizeKeys(obj map[string]any, priority []string) []string {
	seen := map[string]bool{}
	var ordered []string
	for _, k := range priority {
		if _, ok := obj[k]; ok {
			ordered = append(ordered, k)
			seen[k] = true
		}
	}
	remaining := make([]string, 0, len(obj)-len(ordered))
	for k := range obj {
		if !seen[k] {
			remaining = append(remaining, k)
		}
	}
	sort.Strings(remaining)
	return append(ordered, remaining...)
}
