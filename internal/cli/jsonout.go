package cli

import "github.com/spf13/cobra"

// jsonRequested reports whether --json was asked for.
//
// The DTOs themselves live in internal/dto, not here: they are a wire
// contract shared with the HTTP API, and a contract that exists once per
// consumer drifts. Only this cobra lookup is CLI-specific.
func jsonRequested(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool(flagJSON)

	return v
}
