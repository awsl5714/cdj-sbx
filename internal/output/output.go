// Package output renders human and JSON results and maps error kinds to stable
// exit codes for scripts and agents.
package output

import (
	"encoding/json"
	"io"
	"strings"
)

// Stable exit codes (see docs/specs).
const (
	ExitOK        = 0
	ExitGeneric   = 1
	ExitUsage     = 2
	ExitSchema    = 3
	ExitInvariant = 4
	ExitIO        = 5
	ExitReload    = 6
	ExitLock      = 7
)

// Envelope is the stable JSON output shape.
type Envelope struct {
	OK     bool        `json:"ok"`
	Action string      `json:"action,omitempty"`
	DryRun bool        `json:"dry_run,omitempty"`
	Result interface{} `json:"result,omitempty"`
	Error  *ErrInfo    `json:"error,omitempty"`
}

// ErrInfo is the machine-readable error body.
type ErrInfo struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// UsageError is a CLI usage problem (bad/missing args), mapped to ExitUsage.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// EmitJSON writes the envelope as indented JSON.
func EmitJSON(w io.Writer, env Envelope) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(env)
}

// ExitCodeForKind maps an apply error kind to an exit code.
func ExitCodeForKind(kind string) int {
	switch {
	case strings.HasPrefix(kind, "invariant_violated"):
		return ExitInvariant
	case kind == "schema_invalid":
		return ExitSchema
	case kind == "reload_failed":
		return ExitReload
	case kind == "lock_timeout":
		return ExitLock
	case kind == "io_error", kind == "inbound_not_found":
		return ExitIO
	default:
		return ExitGeneric
	}
}
