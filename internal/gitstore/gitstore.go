// Package gitstore records config changes in git for diff and rollback. It
// shells out to the system git (no go-git dependency) and is a no-op when
// disabled. Commits use an explicit identity so they work on headless servers
// without global git config.
package gitstore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Store commits files under Dir when Enabled.
type Store struct {
	Dir     string
	Enabled bool
}

// New constructs a Store.
func New(dir string, enabled bool) *Store {
	return &Store{Dir: dir, Enabled: enabled}
}

// EnsureInit runs `git init` if Dir is not already a repo.
func (s *Store) EnsureInit() error {
	if s == nil || !s.Enabled {
		return nil
	}
	if _, err := os.Stat(filepath.Join(s.Dir, ".git")); err == nil {
		return nil
	}
	return s.run("init")
}

// Commit stages paths and commits them. It is a no-op if nothing is staged.
func (s *Store) Commit(message string, paths ...string) error {
	if s == nil || !s.Enabled {
		return nil
	}
	if err := s.EnsureInit(); err != nil {
		return err
	}
	args := append([]string{"add"}, paths...)
	if err := s.run(args...); err != nil {
		return err
	}
	if s.noStagedChanges() {
		return nil
	}
	return s.run("-c", "user.name=sbx", "-c", "user.email=sbx@localhost", "commit", "-m", message)
}

func (s *Store) noStagedChanges() bool {
	cmd := exec.Command("git", "-C", s.Dir, "diff", "--cached", "--quiet")
	return cmd.Run() == nil // exit 0 => no staged changes
}

func (s *Store) run(args ...string) error {
	full := append([]string{"-C", s.Dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}
