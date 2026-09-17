package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/RobertsMattL/chainsaw/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage log configurations",
}

func init() {
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configAddCmd)
	configCmd.AddCommand(configRemoveCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configEditCmd)
}

// — styles —

var (
	cfgTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	cfgDimStyle    = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("244"))
	cfgNameStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	cfgLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true)
	cfgOkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	cfgErrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	cfgPromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
)

// — list —

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved configurations",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := config.Load()
		if err != nil {
			return err
		}
		if len(root.Configs) == 0 {
			fmt.Println(cfgDimStyle.Render("No configurations saved. Run 'chainsaw config add' to create one."))
			return nil
		}

		fmt.Println()
		fmt.Println(cfgTitleStyle.Render("⛓  Saved configurations"))
		fmt.Println()

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, cfgLabelStyle.Render("NAME\tSOURCES\tJSON\tDESCRIPTION"))
		for _, lc := range root.Configs {
			jsonTag := cfgDimStyle.Render("-")
			if lc.JSONMode {
				jsonTag = cfgOkStyle.Render("✓")
			}
			desc := lc.Description
			if desc == "" {
				desc = cfgDimStyle.Render("—")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				cfgNameStyle.Render(lc.Name),
				cfgDimStyle.Render(fmt.Sprintf("%d", len(lc.Sources))),
				jsonTag,
				desc,
			)
		}
		w.Flush()
		fmt.Println()
		fmt.Println(cfgDimStyle.Render("Run: chainsaw watch <name>"))
		return nil
	},
}

// — add —

var configAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new configuration interactively",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := config.Load()
		if err != nil {
			return err
		}

		in := bufio.NewReader(os.Stdin)
		prompt := func(q string) string {
			fmt.Print(cfgPromptStyle.Render(q))
			line, _ := in.ReadString('\n')
			return strings.TrimSpace(line)
		}
		promptDefault := func(q, def string) string {
			fmt.Printf("%s%s", cfgPromptStyle.Render(q), cfgDimStyle.Render("("+def+") "))
			line, _ := in.ReadString('\n')
			s := strings.TrimSpace(line)
			if s == "" {
				return def
			}
			return s
		}

		fmt.Println()
		fmt.Println(cfgTitleStyle.Render("⛓  New configuration"))
		fmt.Println()

		name := prompt("Name: ")
		if name == "" {
			return fmt.Errorf("name cannot be empty")
		}
		if root.Find(name) != nil {
			return fmt.Errorf("a config named %q already exists", name)
		}

		desc := prompt("Description (optional): ")

		lc := config.LogConfig{Name: name, Description: desc}

		fmt.Println()
		fmt.Println(cfgDimStyle.Render("Add sources. Enter an empty line when done."))
		fmt.Println(cfgDimStyle.Render("Source types: file (path or glob), command (shell command), ssh (remote tail)"))
		fmt.Println()

		for i := 1; ; i++ {
			fmt.Printf(cfgDimStyle.Render("Source %d\n"), i)
			sourceType := promptDefault("  Type [file/command/ssh]: ", "file")
			sourceType = strings.ToLower(sourceType)
			if sourceType != "file" && sourceType != "command" && sourceType != "ssh" {
				sourceType = "file"
			}

			var s config.Source
			s.Type = sourceType

			switch sourceType {
			case "file":
				s.Path = prompt("  Path (glob ok): ")
				if s.Path == "" {
					if i == 1 {
						fmt.Println(cfgErrStyle.Render("At least one source is required."))
						i--
						continue
					}
					goto done
				}
			case "command":
				s.Command = prompt("  Command: ")
				if s.Command == "" {
					if i == 1 {
						fmt.Println(cfgErrStyle.Render("At least one source is required."))
						i--
						continue
					}
					goto done
				}
			case "ssh":
				s.Host = prompt("  Host (user@host or IP): ")
				if s.Host == "" {
					if i == 1 {
						fmt.Println(cfgErrStyle.Render("At least one source is required."))
						i--
						continue
					}
					goto done
				}
				s.Command = prompt("  Remote command: ")
				if s.Command == "" {
					fmt.Println(cfgErrStyle.Render("Remote command cannot be empty."))
					i--
					continue
				}
			}

			s.Label = prompt("  Label (optional, for multi-source display): ")
			lc.Sources = append(lc.Sources, s)

			more := promptDefault("  Add another source? ", "n")
			if !strings.EqualFold(more, "y") {
				break
			}
			fmt.Println()
		}
	done:

		fmt.Println()
		useDefaults := promptDefault("Use default log-level highlighting (ERROR/WARN/INFO/DEBUG)? ", "y")
		if !strings.EqualFold(useDefaults, "n") {
			lc.Highlights = config.DefaultHighlights()
		} else {
			fmt.Println(cfgDimStyle.Render("Skipped. Edit the config YAML to add custom highlight rules."))
		}

		jsonMode := promptDefault("Enable JSON log parsing? ", "n")
		lc.JSONMode = strings.EqualFold(jsonMode, "y")

		root.Configs = append(root.Configs, lc)
		if err := config.Save(root); err != nil {
			return err
		}

		fmt.Println()
		fmt.Printf("%s  Configuration %s saved\n",
			cfgOkStyle.Render("✓"),
			cfgNameStyle.Render(name),
		)
		fmt.Printf("   Run: %s\n\n", lipgloss.NewStyle().Bold(true).Render("chainsaw watch "+name))
		return nil
	},
}

// — remove —

var configRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a configuration",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		root, err := config.Load()
		if err != nil {
			return err
		}

		idx := -1
		for i, lc := range root.Configs {
			if lc.Name == name {
				idx = i
				break
			}
		}
		if idx == -1 {
			return fmt.Errorf("config %q not found", name)
		}

		root.Configs = append(root.Configs[:idx], root.Configs[idx+1:]...)
		if err := config.Save(root); err != nil {
			return err
		}

		fmt.Printf("%s  Removed %s\n", cfgOkStyle.Render("✓"), cfgNameStyle.Render(name))
		return nil
	},
}

// — show —

var configShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show configuration details as YAML",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := config.Load()
		if err != nil {
			return err
		}
		lc := root.Find(args[0])
		if lc == nil {
			return fmt.Errorf("config %q not found", args[0])
		}

		b, err := yaml.Marshal(lc)
		if err != nil {
			return err
		}

		fmt.Println()
		fmt.Println(cfgTitleStyle.Render(lc.Name))
		fmt.Println(cfgDimStyle.Render("─────────────────────────────────────────"))
		fmt.Println(string(b))
		fmt.Printf("%s  %s\n\n", cfgDimStyle.Render("config file:"), config.Path())
		return nil
	},
}

// — edit —

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open the config file in $EDITOR",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := config.Path()
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			editor = "vi"
		}
		c := exec.Command(editor, path)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	},
}
