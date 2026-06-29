package apply

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/awsl5714/cdj-sbx/internal/config"
	"github.com/awsl5714/cdj-sbx/internal/gitstore"
	"github.com/awsl5714/cdj-sbx/internal/validate"
)

const testServerCfg = `{
  "inbounds": [
    {
      "type": "vless",
      "tag": "reality-in",
      "listen_port": 443,
      "users": [ { "name": "alice", "uuid": "u-alice", "flow": "xtls-rprx-vision" } ],
      "tls": { "server_name": "www.apple.com", "reality": { "private_key": "PRIV", "short_id": ["abcd"] } }
    },
    {
      "type": "hysteria2",
      "tag": "hy2-in",
      "listen_port": 443,
      "users": [ { "name": "alice", "password": "u-alice" } ],
      "obfs": { "type": "salamander", "password": "obfspw" }
    }
  ],
  "outbounds": [ { "type": "direct", "tag": "direct" } ]
}`

// writeStub creates a fake `sing-box`: exit 0 (valid) or exit 3 (invalid).
func writeStub(t *testing.T, dir string, ok bool) string {
	t.Helper()
	code := 0
	if !ok {
		code = 3
	}
	p := filepath.Join(dir, "fake-sing-box")
	if err := os.WriteFile(p, []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", code)), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func setupEnv(t *testing.T, valid bool) (Options, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(testServerCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return Options{
		ConfigPath: cfg,
		Checker:    validate.New(writeStub(t, dir, valid)),
		Git:        gitstore.New(dir, true),
		Reload:     func() error { return nil },
		Managed:    DefaultManaged(),
	}, cfg
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func gitCommitCount(t *testing.T, dir string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").CombinedOutput()
	if err != nil {
		return 0 // no repo / no commits
	}
	return len(strings.TrimSpace(string(out)))
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, ".sbx-*.json"))
	if len(matches) != 0 {
		t.Fatalf("leftover temp files: %v", matches)
	}
}

func TestAddUserHappyPath(t *testing.T) {
	o, cfg := setupEnv(t, true)
	res, err := AddUser(o, "bob", "u-bob")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || res.DryRun {
		t.Fatalf("unexpected result %+v", res)
	}
	b := mustRead(t, cfg)
	if got := gjson.GetBytes(b, `inbounds.0.users.#(name=="bob").uuid`).String(); got != "u-bob" {
		t.Fatalf("bob not in reality: %q", got)
	}
	if got := gjson.GetBytes(b, `inbounds.1.users.#(name=="bob").password`).String(); got != "u-bob" {
		t.Fatalf("bob not in hy2: %q", got)
	}
	if gitCommitCount(t, filepath.Dir(cfg)) < 1 {
		t.Fatal("expected a git commit")
	}
	assertNoTempFiles(t, filepath.Dir(cfg))
}

func TestDryRunWritesNothing(t *testing.T) {
	o, cfg := setupEnv(t, true)
	o.DryRun = true
	before := mustRead(t, cfg)
	res, err := AddUser(o, "bob", "u-bob")
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Diff == "" {
		t.Fatalf("expected dry-run with diff, got %+v", res)
	}
	if !bytes.Equal(before, mustRead(t, cfg)) {
		t.Fatal("dry-run modified the live config")
	}
	assertNoTempFiles(t, filepath.Dir(cfg))
}

func TestDuplicateUserAborts(t *testing.T) {
	o, cfg := setupEnv(t, true)
	before := mustRead(t, cfg)
	_, err := AddUser(o, "alice", "x") // alice already exists
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != KindDuplicateUser {
		t.Fatalf("want duplicate_user, got %v", err)
	}
	if !bytes.Equal(before, mustRead(t, cfg)) {
		t.Fatal("file changed on duplicate")
	}
}

func TestSchemaInvalidAbortsAtomically(t *testing.T) {
	o, cfg := setupEnv(t, false) // stub exits 3
	before := mustRead(t, cfg)
	_, err := AddUser(o, "bob", "u-bob")
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != KindSchema {
		t.Fatalf("want schema_invalid, got %v", err)
	}
	if !bytes.Equal(before, mustRead(t, cfg)) {
		t.Fatal("live config changed despite schema failure (atomicity broken)")
	}
	assertNoTempFiles(t, filepath.Dir(cfg))
}

func TestReloadFailureRollsBackLiveConfig(t *testing.T) {
	o, cfg := setupEnv(t, true)
	o.Reload = func() error { return errors.New("reload boom") }
	before := mustRead(t, cfg)

	_, err := AddUser(o, "bob", "u-bob")
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != KindReload {
		t.Fatalf("want reload_failed, got %v", err)
	}
	if !bytes.Equal(before, mustRead(t, cfg)) {
		t.Fatal("live config changed despite reload failure rollback")
	}
	assertNoTempFiles(t, filepath.Dir(cfg))
}

func TestGitCommitFailureReturnsError(t *testing.T) {
	o, cfg := setupEnv(t, true)
	before := mustRead(t, cfg)
	dir := filepath.Dir(cfg)
	if err := o.Git.Commit("baseline", cfg); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	reloads := 0
	o.Reload = func() error {
		reloads++
		return nil
	}

	_, err := AddUser(o, "bob", "u-bob")
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != KindIO {
		t.Fatalf("want io_error for git commit failure, got %v", err)
	}
	if !strings.Contains(ae.Detail, "git commit failed") {
		t.Fatalf("error detail should mention git commit failure, got %q", ae.Detail)
	}
	if !bytes.Equal(before, mustRead(t, cfg)) {
		t.Fatal("live config changed despite git commit failure rollback")
	}
	if reloads != 2 {
		t.Fatalf("want apply reload and rollback reload, got %d reloads", reloads)
	}
	if out, err := exec.Command("git", "-C", dir, "diff", "--cached", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git index still has staged rollback candidate: %v: %s", err, out)
	}
}

func TestDelUser(t *testing.T) {
	o, cfg := setupEnv(t, true)
	if _, err := AddUser(o, "bob", "u-bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := DelUser(o, "bob"); err != nil {
		t.Fatal(err)
	}
	b := mustRead(t, cfg)
	if gjson.GetBytes(b, `inbounds.0.users.#(name=="bob")`).Exists() {
		t.Fatal("bob still in reality")
	}
	if gjson.GetBytes(b, `inbounds.1.users.#(name=="bob")`).Exists() {
		t.Fatal("bob still in hy2")
	}
}

func TestDelMissingUser(t *testing.T) {
	o, _ := setupEnv(t, true)
	_, err := DelUser(o, "ghost")
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != KindUserNotFound {
		t.Fatalf("want user_not_found, got %v", err)
	}
}

func TestVerifyDetectsInvariant(t *testing.T) {
	o, cfg := setupEnv(t, true)
	// Break I1: remove alice from only the hy2 inbound, directly on disk.
	c := config.New(mustRead(t, cfg))
	if err := c.RemoveUser("hy2-in", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, c.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Verify(o)
	var ae *Error
	if !errors.As(err, &ae) || !strings.HasPrefix(ae.Kind, "invariant_violated") {
		t.Fatalf("want invariant_violated, got %v", err)
	}
}

func TestVerifyOK(t *testing.T) {
	o, _ := setupEnv(t, true)
	if err := Verify(o); err != nil {
		t.Fatalf("want clean verify, got %v", err)
	}
}
