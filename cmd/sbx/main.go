// Command sbx is a safety-first CLI for managing sing-box server configs.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cdj/sbx/internal/apply"
	"github.com/cdj/sbx/internal/gitstore"
	"github.com/cdj/sbx/internal/output"
	"github.com/cdj/sbx/internal/validate"
)

var (
	cfgPath    string
	jsonOut    bool
	singboxBin string
	noGit      bool
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		emitError(err)
		os.Exit(exitCode(err))
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "sbx",
		Short:         "Safety-first CLI for sing-box server configs",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	pf := root.PersistentFlags()
	pf.StringVar(&cfgPath, "config", "/etc/sing-box/config.json", "path to sing-box config.json")
	pf.BoolVar(&jsonOut, "json", false, "machine-readable JSON output")
	pf.StringVar(&singboxBin, "singbox-bin", "sing-box", "path to the sing-box binary")
	pf.BoolVar(&noGit, "no-git", false, "disable git commit/rollback")
	root.AddCommand(newInitCmd(), newUserCmd(), newVerifyCmd(), newLinkCmd(), newReloadCmd())
	return root
}

func buildOptions(dryRun, noReload bool) apply.Options {
	return apply.Options{
		ConfigPath: cfgPath,
		Checker:    validate.New(singboxBin),
		Git:        gitstore.New(filepath.Dir(cfgPath), !noGit),
		Managed:    apply.DefaultManaged(),
		DryRun:     dryRun,
		NoReload:   noReload,
	}
}

func exitCode(err error) int {
	var ue *output.UsageError
	if errors.As(err, &ue) {
		return output.ExitUsage
	}
	var ae *apply.Error
	if errors.As(err, &ae) {
		return output.ExitCodeForKind(ae.Kind)
	}
	return output.ExitGeneric
}

func emitError(err error) {
	kind := "error"
	var ae *apply.Error
	if errors.As(err, &ae) {
		kind = ae.Kind
	}
	var ue *output.UsageError
	if errors.As(err, &ue) {
		kind = "usage"
	}
	if jsonOut {
		output.EmitJSON(os.Stderr, output.Envelope{OK: false, Error: &output.ErrInfo{Kind: kind, Detail: err.Error()}})
		return
	}
	fmt.Fprintln(os.Stderr, "error:", err.Error())
}
