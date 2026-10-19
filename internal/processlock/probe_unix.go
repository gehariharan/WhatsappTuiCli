//go:build darwin || linux

package processlock

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Held reports whether another process holds the lock at path. It never
// creates, chmods or keeps the lock: a missing file means no owner, and the
// shared probe lock is released before returning. Two probes never block each
// other; an owner starting in the same instant may see ErrBusy once.
func Held(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("refusing non-regular lock file %s", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, fmt.Errorf("open process lock: %w", err)
	}
	defer unix.Close(fd)
	if err = unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return true, nil
		}
		return false, fmt.Errorf("probe process lock: %w", err)
	}
	return false, unix.Flock(fd, unix.LOCK_UN)
}
