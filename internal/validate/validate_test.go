package validate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckFileClassifiesFailures(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "ok")
	if err := os.WriteFile(good, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\necho boom 1>&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := New(good).CheckFile("cfg"); err != nil {
		t.Fatalf("valid config: want nil, got %v", err)
	}

	err := New(bad).CheckFile("cfg")
	var ce *CheckError
	if !errors.As(err, &ce) {
		t.Fatalf("nonzero exit: want *CheckError, got %T (%v)", err, err)
	}

	err = New(filepath.Join(dir, "missing")).CheckFile("cfg")
	var ee *ExecError
	if !errors.As(err, &ee) {
		t.Fatalf("missing binary: want *ExecError, got %T (%v)", err, err)
	}
}
