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

// ExecError means the validator binary could not be executed (missing, not
// executable, ...) — an environment failure, distinct from the validator
// running and rejecting the config (*CheckError). Callers map it to io_error,
// not schema_invalid.
type ExecError struct {
	Bin string
	Err error
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("could not run validator %q: %v", e.Bin, e.Err)
}

func (e *ExecError) Unwrap() error { return e.Err }

// CheckFile runs `sing-box check -c <path>`. It returns *CheckError when the
// validator ran and rejected the config, and *ExecError when the validator
// could not be executed at all.
func (c *Checker) CheckFile(path string) error {
	cmd := exec.Command(c.Bin, "check", "-c", path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &CheckError{Output: strings.TrimSpace(string(out))}
	}
	return &ExecError{Bin: c.Bin, Err: err}
}
