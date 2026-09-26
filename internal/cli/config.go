package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/config"
)

// The config command group never touches the database: it operates purely
// on the resolved settings, so PersistentPreRunE skips the boot sequence.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and modify application configuration",
	Args:  cobra.NoArgs,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the location of the config file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, explicit, err := config.DiscoverPath(os.Getenv)
		if err != nil {
			return err
		}

		cmd.Println(path)
		if !explicit {
			cmd.PrintErrln("Note: file does not exist yet; `yci config set` creates it on first write.")
		}

		return nil
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configurable keys with their resolved values and sources",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(os.Getenv)
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}

		w := newTabWriter(cmd.OutOrStdout())
		fmt.Fprintln(w, "KEY\tVALUE\tSOURCE")
		fmt.Fprintln(w, "---\t-----\t------")

		for _, name := range config.Keys() {
			value, source, err := cfg.Resolved(name)
			if err != nil {
				return err
			}
			if source == config.SourceUnset {
				source = config.SourceDefault
			}

			display := value
			if value == "" {
				display = "(unset)"
			}

			fmt.Fprintf(w, "%s\t%s\t%s\n", name, display, source)
		}

		w.Flush()
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:               "get <key>",
	Short:             "Print the resolved value of a single key",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: configKeyCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(os.Getenv)
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}

		value, source, err := cfg.Resolved(args[0])
		if err != nil {
			return err
		}
		if source == config.SourceUnset {
			source = config.SourceDefault
		}

		cmd.Printf("%s (from %s)\n", value, source)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:               "set <key> <value>",
	Short:             "Validate and persist a config value (rewrites the file without comments)",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: configKeyCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.Set(os.Getenv, args[0], args[1]); err != nil {
			return err
		}

		cmd.Printf("Set %s = %s\n", args[0], args[1])
		cmd.Println("Warning: comments in the config file were removed.")
		return nil
	},
}

func configKeyCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var comps []string
	for _, k := range config.Keys() {
		if len(args) == 0 && strings.HasPrefix(k, toComplete) {
			comps = append(comps, k)
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
}
