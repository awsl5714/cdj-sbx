// Package apply is the mutation pipeline that ties config surgery, schema
// validation, semantic invariants, atomic write, reload, and git together.
//
// Pipeline (add/del share it):
//
//	flock -> load -> mutate (in memory) -> write candidate to same-dir temp
//	      -> sing-box check -> invariant check -> [dry-run: stop]
//	      -> atomic rename -> reload -> git commit
//
// Anything before the rename fails closed: the live config is never touched.
package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/awsl5714/cdj-sbx/internal/config"
	"github.com/awsl5714/cdj-sbx/internal/gitstore"
	"github.com/awsl5714/cdj-sbx/internal/invariant"
	"github.com/awsl5714/cdj-sbx/internal/model"
	"github.com/awsl5714/cdj-sbx/internal/validate"
)

// Error kinds (machine-readable; see internal/output for exit-code mapping).
const (
	KindSchema          = "schema_invalid"
	KindInvariant       = "invariant_violated"
	KindDuplicateUser   = "duplicate_user"
	KindUserNotFound    = "user_not_found"
	KindInboundNotFound = "inbound_not_found"
	KindReload          = "reload_failed"
	KindLockTimeout     = "lock_timeout"
	KindIO              = "io_error"
)

// Error is a pipeline failure carrying a machine-readable Kind.
type Error struct {
	Kind   string
	Detail string
	Err    error
}

func (e *Error) Error() string { return e.Kind + ": " + e.Detail }
func (e *Error) Unwrap() error { return e.Err }

// Managed describes one inbound kept in sync, and how to build its user object.
type Managed struct {
	Tag         string
	SecretField string
	Fields      func(name, secret string) []config.Field
}

// DefaultManaged returns the v1 pairing: reality-in (uuid + vision flow) and
// hy2-in (password).
func DefaultManaged() []Managed {
	return []Managed{
		{Tag: "reality-in", SecretField: "uuid", Fields: func(n, s string) []config.Field {
			return []config.Field{{Key: "name", Val: n}, {Key: "uuid", Val: s}, {Key: "flow", Val: "xtls-rprx-vision"}}
		}},
		{Tag: "hy2-in", SecretField: "password", Fields: func(n, s string) []config.Field {
			return []config.Field{{Key: "name", Val: n}, {Key: "password", Val: s}}
		}},
	}
}

// Options configures the pipeline. Reload is injectable for tests.
type Options struct {
	ConfigPath string
	Checker    *validate.Checker
	Git        *gitstore.Store
	Reload     func() error
	DryRun     bool
	NoReload   bool
	Managed    []Managed
	LockPath   string
}

// Result describes the outcome of a mutating command.
type Result struct {
	Action   string   `json:"action"`
	User     string   `json:"user,omitempty"`
	UUID     string   `json:"uuid,omitempty"`
	DryRun   bool     `json:"dry_run"`
	Applied  bool     `json:"applied"`
	Diff     string   `json:"diff,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type mutation func(c *config.Config) error

// AddUser adds a user to every managed inbound, sharing one secret (uuid).
func AddUser(o Options, name, uuid string) (*Result, error) {
	return run(o, "user.add", name, uuid, func(c *config.Config) error {
		for _, m := range o.Managed {
			has, err := c.HasUser(m.Tag, name)
			if err != nil {
				return err
			}
			if has {
				return fmt.Errorf("%w: %s", config.ErrUserExists, name)
			}
		}
		for _, m := range o.Managed {
			if err := c.AppendUser(m.Tag, m.Fields(name, uuid)); err != nil {
				return err
			}
		}
		return nil
	})
}

// DelUser removes a user from every managed inbound. A user present in only one
// inbound (a pre-existing I1 violation) is still cleaned from that one.
func DelUser(o Options, name string) (*Result, error) {
	return run(o, "user.del", name, "", func(c *config.Config) error {
		removed := false
		for _, m := range o.Managed {
			err := c.RemoveUser(m.Tag, name)
			switch {
			case err == nil:
				removed = true
			case errors.Is(err, config.ErrUserNotFound):
				// tolerate absence in this inbound
			default:
				return err
			}
		}
		if !removed {
			return fmt.Errorf("%w: %s", config.ErrUserNotFound, name)
		}
		return nil
	})
}

// ListUsers returns the managed users, using the first managed inbound (reality)
// as the source of truth.
func ListUsers(o Options) ([]model.User, error) {
	c, err := config.Load(o.ConfigPath)
	if err != nil {
		return nil, &Error{Kind: KindIO, Detail: err.Error(), Err: err}
	}
	users, err := c.Users(o.Managed[0].Tag, o.Managed[0].SecretField)
	if err != nil {
		return nil, mapMutateErr(err)
	}
	return users, nil
}

// Verify runs schema + invariant checks against the live config without mutating.
func Verify(o Options) error {
	if err := o.Checker.CheckFile(o.ConfigPath); err != nil {
		return checkErr(err)
	}
	c, err := config.Load(o.ConfigPath)
	if err != nil {
		return &Error{Kind: KindIO, Detail: err.Error(), Err: err}
	}
	return invariantErr(c, o.Managed)
}

// Reload validates the live config then reloads the service.
func Reload(o Options) error {
	if err := o.Checker.CheckFile(o.ConfigPath); err != nil {
		return checkErr(err)
	}
	c, err := config.Load(o.ConfigPath)
	if err != nil {
		return &Error{Kind: KindIO, Detail: err.Error(), Err: err}
	}
	if err := invariantErr(c, o.Managed); err != nil {
		return err
	}
	if err := o.reload(); err != nil {
		return &Error{Kind: KindReload, Detail: err.Error(), Err: err}
	}
	return nil
}

func run(o Options, action, user, uuid string, mut mutation) (*Result, error) {
	unlock, err := acquireLock(o.lockPath())
	if err != nil {
		if errors.Is(err, errLocked) {
			return nil, &Error{Kind: KindLockTimeout, Detail: "another sbx process holds the lock", Err: err}
		}
		return nil, &Error{Kind: KindIO, Detail: "cannot acquire lock: " + err.Error(), Err: err}
	}
	defer unlock()

	c, err := config.Load(o.ConfigPath)
	if err != nil {
		return nil, &Error{Kind: KindIO, Detail: err.Error(), Err: err}
	}
	old := append([]byte(nil), c.Bytes()...)

	if err := mut(c); err != nil {
		return nil, mapMutateErr(err)
	}

	tmp, err := writeTemp(o.ConfigPath, c.Bytes())
	if err != nil {
		return nil, &Error{Kind: KindIO, Detail: err.Error(), Err: err}
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmp)
		}
	}()

	if err := o.Checker.CheckFile(tmp); err != nil {
		return nil, checkErr(err)
	}
	if err := invariantErr(c, o.Managed); err != nil {
		return nil, err
	}

	res := &Result{Action: action, User: user, UUID: uuid}
	if o.DryRun {
		res.DryRun = true
		res.Diff = lineDiff(string(old), string(c.Bytes()))
		return res, nil
	}

	preserveOwnerMode(tmp, o.ConfigPath)
	if err := os.Rename(tmp, o.ConfigPath); err != nil {
		return nil, &Error{Kind: KindIO, Detail: err.Error(), Err: err}
	}
	removeTmp = false
	fsyncDir(o.ConfigPath)

	if !o.NoReload {
		if err := o.reload(); err != nil {
			detail := err.Error()
			if rbErr := rollbackApplied(o, old); rbErr != nil {
				detail += "; rollback failed: " + rbErr.Error()
			}
			return nil, &Error{Kind: KindReload, Detail: detail, Err: err}
		}
	}
	if o.Git != nil && o.Git.Enabled {
		if err := o.Git.Commit(commitMsg(action, user), o.ConfigPath); err != nil {
			detail := "git commit failed: " + err.Error()
			if rbErr := rollbackApplied(o, old); rbErr != nil {
				detail += "; rollback failed: " + rbErr.Error()
			} else if stageErr := o.Git.Stage(o.ConfigPath); stageErr != nil {
				detail += "; git index restore failed: " + stageErr.Error()
			}
			return nil, &Error{Kind: KindIO, Detail: detail, Err: err}
		}
	}
	res.Applied = true
	return res, nil
}

func rollbackApplied(o Options, old []byte) error {
	if err := rollbackConfig(o.ConfigPath, old); err != nil {
		return err
	}
	if o.NoReload {
		return nil
	}
	if err := o.reload(); err != nil {
		return fmt.Errorf("rollback reload failed: %w", err)
	}
	return nil
}

func rollbackConfig(path string, old []byte) error {
	tmp, err := writeTemp(path, old)
	if err != nil {
		return err
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmp)
		}
	}()

	preserveOwnerMode(tmp, path)
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	removeTmp = false
	fsyncDir(path)
	return nil
}

// fsyncDir flushes the parent directory entry so a rename is durable across a
// crash or power loss. Best-effort: durability is improved, never required for
// the call to succeed.
func fsyncDir(path string) {
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func (o Options) reload() error {
	if o.Reload != nil {
		return o.Reload()
	}
	return DefaultReload()
}

func (o Options) lockPath() string {
	if o.LockPath != "" {
		return o.LockPath
	}
	return filepath.Join(filepath.Dir(o.ConfigPath), ".sbx.lock")
}

func invariantErr(c *config.Config, managed []Managed) error {
	if len(managed) != 2 {
		return &Error{Kind: KindIO, Detail: fmt.Sprintf("expected 2 managed inbounds, got %d", len(managed))}
	}
	ru, err := c.Users(managed[0].Tag, managed[0].SecretField)
	if err != nil {
		return mapMutateErr(err)
	}
	hu, err := c.Users(managed[1].Tag, managed[1].SecretField)
	if err != nil {
		return mapMutateErr(err)
	}
	if err := invariant.Check(ru, hu); err != nil {
		kind := KindInvariant
		var v *invariant.Violation
		if errors.As(err, &v) {
			kind = KindInvariant + ":" + v.ID
		}
		return &Error{Kind: kind, Detail: err.Error(), Err: err}
	}
	return nil
}

// checkErr maps a CheckFile failure to the right kind: a validator that ran and
// rejected the config is schema_invalid; a validator that could not run at all
// (missing binary, etc.) is an environment/io error.
func checkErr(err error) *Error {
	var ee *validate.ExecError
	if errors.As(err, &ee) {
		return &Error{Kind: KindIO, Detail: err.Error(), Err: err}
	}
	return &Error{Kind: KindSchema, Detail: err.Error(), Err: err}
}

func mapMutateErr(err error) *Error {
	switch {
	case errors.Is(err, config.ErrUserExists):
		return &Error{Kind: KindDuplicateUser, Detail: err.Error(), Err: err}
	case errors.Is(err, config.ErrUserNotFound):
		return &Error{Kind: KindUserNotFound, Detail: err.Error(), Err: err}
	case errors.Is(err, config.ErrInboundNotFound):
		return &Error{Kind: KindInboundNotFound, Detail: err.Error(), Err: err}
	default:
		return &Error{Kind: KindIO, Detail: err.Error(), Err: err}
	}
}

func writeTemp(cfgPath string, data []byte) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(cfgPath), ".sbx-*.json")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func commitMsg(action, user string) string {
	switch action {
	case "user.add":
		return "sbx: add user " + user
	case "user.del":
		return "sbx: del user " + user
	default:
		return "sbx: " + action
	}
}

// lineDiff produces a compact +/- diff by trimming the common prefix/suffix.
// sjson edits are localized, so this yields a tight, readable preview.
func lineDiff(oldStr, newStr string) string {
	o := strings.Split(oldStr, "\n")
	n := strings.Split(newStr, "\n")
	p := 0
	for p < len(o) && p < len(n) && o[p] == n[p] {
		p++
	}
	s := 0
	for s < len(o)-p && s < len(n)-p && o[len(o)-1-s] == n[len(n)-1-s] {
		s++
	}
	var b strings.Builder
	for i := p; i < len(o)-s; i++ {
		b.WriteString("- " + o[i] + "\n")
	}
	for i := p; i < len(n)-s; i++ {
		b.WriteString("+ " + n[i] + "\n")
	}
	return b.String()
}
