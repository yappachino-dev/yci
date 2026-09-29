package cli

import (
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// formWidth is the render width of the interactive forms. huh does not
// default it, and a zero-width input panics in bubbles' placeholder view on
// first draw.
const formWidth = 72

// newForm builds a huh form wired to a command's input and output.
//
// The obvious implementation, huh.NewForm(...), reaches for os.Stdin and
// os.Stdout directly, which is why this package's interactive paths were
// untestable: a test could not hand the form a scripted keypress, so
// "type a number, press enter, confirm" had never actually been executed.
// Going through cmd.InOrStdin / cmd.OutOrStdout makes the same code path
// drivable by a test, and changes nothing for a real terminal where those
// resolve to the process streams.
func newForm(cmd *cobra.Command, fields ...huh.Field) *huh.Form {
	return huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(huh.ThemeBase()).
		WithWidth(formWidth).
		WithInput(cmd.InOrStdin()).
		WithOutput(cmd.OutOrStdout())
}

// inputIsTTY reports whether a command's standard input is an interactive
// terminal.
//
// It takes the command rather than reading the process stdin so that the
// guard describes the stream the form will actually read. Checking one and
// reading the other is how a test ends up passing the guard and then hanging.
func inputIsTTY(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(interface{ Fd() uintptr })
	if !ok {
		return false
	}

	return term.IsTerminal(int(f.Fd()))
}

// outputIsTTY reports whether a command's standard output is a terminal, which
// is what makes rendered columns worth aligning.
func outputIsTTY(cmd *cobra.Command) bool {
	f, ok := cmd.OutOrStdout().(interface{ Fd() uintptr })
	if !ok {
		return false
	}

	return term.IsTerminal(int(f.Fd()))
}

// flagChanged reports whether the user named a flag explicitly rather than it
// holding its default.
func flagChanged(cmd *cobra.Command, name string) bool {
	return cmd.Flags().Changed(name)
}
