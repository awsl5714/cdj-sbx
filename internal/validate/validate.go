// Package validate wraps `sing-box check` — the upstream schema authority.
// sbx never reimplements schema validation; it shells out to the real binary.
package validate

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Checker runs `sing-box check` against a config file.
type Checker struct {
	Bin string
}

// New returns a Checker. An empty bin defaults to "sing-box" (resolved on PATH).
func New(bin string) *Checker {
	if bin == "" {
		bin = "sing-box"
	}
	return &Checker{Bin: bin}
}

// CheckError reports a failed schema validation.
type CheckError struct {
	Output string
}

func (e *CheckError) Error() string {
	if e.Output != "" {
		return "sing-box check failed: " + e.Output
	}
	return "sing-box check failed"
}

// CheckFile runs `sing-box check -c <path>` and returns a *CheckError on failure.
func (c *Checker) CheckFile(path string) error {
	cmd := exec.Command(c.Bin, "check", "-c", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return &CheckError{Output: fmt.Sprintf("binary %q not found on PATH", c.Bin)}
		}
		return &CheckError{Output: strings.TrimSpace(string(out))}
	}
	return nil
}
