package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"github.com/RobertsMattL/chainsaw/internal/config"
	"github.com/RobertsMattL/chainsaw/internal/highlight"
	"github.com/RobertsMattL/chainsaw/internal/source"
	"github.com/RobertsMattL/chainsaw/internal/ui"
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
Press / to filter  ·  q to quit.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatch,
}

func init() {
	watchCmd.Flags().BoolVar(&watchFlags.noColor, "no-color", false, "disable color output")
	watchCmd.Flags().BoolVar(&watchFlags.jsonMode, "json", false, "force JSON mode for all sources")
	watchCmd.Flags().StringVar(&watchFlags.include, "include", "", "only show lines matching this regex (pre-filter)")
	watchCmd.Flags().StringVar(&watchFlags.exclude, "exclude", "", "hide lines matching this regex (pre-filter)")
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
			return nil
		}
	}

	// Apply flag overrides onto a copy
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

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigs; cancel() }()

	lines, err := source.Start(ctx, &merged)
	if err != nil {
		return err
	}

	return ui.RunWatcher(ui.WatchConfig{
		LogConfig:  &merged,
		Engine:     engine,
		IncludeRe:  includeRe,
		ExcludeRe:  excludeRe,
		ShowLabels: len(merged.Sources) > 1,
		Lines:      lines,
	})
}
