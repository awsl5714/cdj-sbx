//go:build unix

package apply

import (
	"os"
	"syscall"
)

// acquireLock takes an exclusive, non-blocking flock on path. It returns a
// release function, or an error if the lock is already held (mapped by callers
// to a lock_timeout). Serializing mutations is invariant I5.
func acquireLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
