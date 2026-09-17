package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

var logoStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("212"))

var rootCmd = &cobra.Command{
	Use:     "chainsaw",
	Short:   "Elegant log viewer with saved configurations",
	Version: version,
	Long: logoStyle.Render("⛓  chainsaw") + "\n\n" +
		"A powerful log tailing tool with saved configurations and elegant highlighting.\n\n" +
		"Quick start:\n" +
		"  chainsaw config add        add a named log configuration\n" +
		"  chainsaw watch             pick a config interactively and start tailing\n" +
		"  chainsaw watch <name>      tail a specific saved configuration\n" +
		"  chainsaw config list       list all saved configurations",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(configCmd)
}
