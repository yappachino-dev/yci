package download

import (
	"errors"
	"fmt"
	"os/exec"
)

// ErrDependencyMissing reports that an external tool yci shells out to is not
// installed. It is a sentinel because the CLI maps it to one actionable
// message, which a bare error string cannot be selected on.
var ErrDependencyMissing = errors.New("required program not found")

// CheckDependencies reports the first required external tool that is
// missing.
//
// The tools are checked here rather than assumed, because a missing ffmpeg
// surfaces much later as an unreadable audio file, by which point the cause
// is a long way from the symptom.
func CheckDependencies() error {
	for _, tool := range []string{"yt-dlp", "ffmpeg"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%w: %s is not installed or not in PATH", ErrDependencyMissing, tool)
		}
	}

	return nil
}
