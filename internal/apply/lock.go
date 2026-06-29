//go:build unix

package apply

import (
	"errors"
	"os"
	"syscall"
)

// errLocked means the lock is held by another process (flock contention), as
// opposed to an I/O failure (e.g. an unwritable directory) when opening the
// lock file. Callers map the former to lock_timeout and the latter to io_error.
var errLocked = errors.New("lock held by another process")

// acquireLock takes an exclusive, non-blocking flock on path. It returns a
// release function, errLocked if another process holds it, or the underlying
// I/O error if the lock file cannot be opened. Serializing mutations is I5.
func acquireLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, errLocked
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
