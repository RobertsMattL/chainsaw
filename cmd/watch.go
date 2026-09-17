package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/somewearlabs/chainsaw/internal/config"
	"github.com/somewearlabs/chainsaw/internal/highlight"
	"github.com/somewearlabs/chainsaw/internal/source"
	"github.com/somewearlabs/chainsaw/internal/ui"
	"github.com/spf13/cobra"
)

var watchFlags struct {
	noColor    bool
	jsonMode   bool
	include    string
	exclude    string
	noDefaults bool
}

var watchCmd = &cobra.Command{
	Use:   "watch [config-name]",
	Short: "Tail logs using a saved configuration",
	Long: `Tail logs using a saved configuration.

If no config name is given, an interactive selector is shown.
Press Ctrl+C to stop watching.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatch,
}

func init() {
	watchCmd.Flags().BoolVar(&watchFlags.noColor, "no-color", false, "disable color output")
	watchCmd.Flags().BoolVar(&watchFlags.jsonMode, "json", false, "force JSON mode for all sources")
	watchCmd.Flags().StringVar(&watchFlags.include, "include", "", "only show lines matching this regex")
	watchCmd.Flags().StringVar(&watchFlags.exclude, "exclude", "", "hide lines matching this regex")
	watchCmd.Flags().BoolVar(&watchFlags.noDefaults, "no-defaults", false, "skip default log-level highlighting")
}

func runWatch(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	var lc *config.LogConfig

	if len(args) == 1 {
		lc = cfg.Find(args[0])
		if lc == nil {
			return fmt.Errorf("config %q not found — run 'chainsaw config list' to see options", args[0])
		}
	} else {
		lc, err = ui.SelectConfig(cfg.Configs)
		if err != nil {
			return err
		}
		if lc == nil {
			return nil // user cancelled
		}
	}

	// Apply flag overrides
	merged := *lc
	if watchFlags.jsonMode {
		merged.JSONMode = true
	}
	if watchFlags.include != "" {
		merged.Filters.Include = watchFlags.include
	}
	if watchFlags.exclude != "" {
		merged.Filters.Exclude = watchFlags.exclude
	}

	// Build highlight rules: use config rules, fall back to defaults
	hlRules := merged.Highlights
	if len(hlRules) == 0 && !watchFlags.noDefaults {
		hlRules = config.DefaultHighlights()
	}
	if watchFlags.noColor {
		hlRules = nil
	}

	engine, err := highlight.New(hlRules, merged.JSONMode)
	if err != nil {
		return fmt.Errorf("highlight engine: %w", err)
	}

	// Compile filters
	var includeRe, excludeRe *regexp.Regexp
	if merged.Filters.Include != "" {
		if includeRe, err = regexp.Compile(merged.Filters.Include); err != nil {
			return fmt.Errorf("include filter: %w", err)
		}
	}
	if merged.Filters.Exclude != "" {
		if excludeRe, err = regexp.Compile(merged.Filters.Exclude); err != nil {
			return fmt.Errorf("exclude filter: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	lines, err := source.Start(ctx, &merged)
	if err != nil {
		return err
	}

	printHeader(&merged)

	// Determine if we need source labels (>1 unique source)
	showLabels := len(merged.Sources) > 1

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Faint(true)

	for line := range lines {
		text := line.Content

		// Apply filters
		if includeRe != nil && !includeRe.MatchString(text) {
			continue
		}
		if excludeRe != nil && excludeRe.MatchString(text) {
			continue
		}

		rendered := engine.Apply(text)

		if showLabels && line.Label != "" {
			prefix := labelStyle.Render("["+line.Label+"] ")
			fmt.Fprintln(out, prefix+rendered)
		} else {
			fmt.Fprintln(out, rendered)
		}
		out.Flush()
	}

	fmt.Fprintln(out, lipgloss.NewStyle().Faint(true).Render("\n⛓  chainsaw stopped"))
	return nil
}

func printHeader(lc *config.LogConfig) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle := lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("244"))
	tagStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236")).
		Padding(0, 1)

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(titleStyle.Render("⛓  chainsaw") + "  ")
	sb.WriteString(tagStyle.Render(lc.Name))
	if lc.Description != "" {
		sb.WriteString("  " + dimStyle.Render(lc.Description))
	}
	sb.WriteString("\n")

	for _, s := range lc.Sources {
		icon := "📄"
		val := s.Path
		if s.Type == "command" {
			icon = "⚡"
			val = s.Command
		}
		label := ""
		if s.Label != "" {
			label = " [" + s.Label + "]"
		}
		sb.WriteString(dimStyle.Render(fmt.Sprintf("   %s %s%s", icon, val, label)) + "\n")
	}

	tags := []string{}
	if lc.JSONMode {
		tags = append(tags, "json")
	}
	if lc.Filters.Include != "" {
		tags = append(tags, "include:"+lc.Filters.Include)
	}
	if lc.Filters.Exclude != "" {
		tags = append(tags, "exclude:"+lc.Filters.Exclude)
	}
	if len(tags) > 0 {
		sb.WriteString(dimStyle.Render("   "+strings.Join(tags, " · ")) + "\n")
	}

	sb.WriteString(dimStyle.Render("─────────────────────────────────────────") + "\n\n")
	fmt.Print(sb.String())
}
