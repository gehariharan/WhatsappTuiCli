//go:build windows

package processlock

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// Held reports whether another process holds the lock at path. It never
// creates, changes or keeps the lock: a missing file means no owner, and the
// shared probe lock is released before returning.
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
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return false, fmt.Errorf("open process lock: %w", err)
	}
	defer windows.CloseHandle(h)
	var overlap windows.Overlapped
	if err = windows.LockFileEx(h, windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlap); err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return true, nil
		}
		return false, fmt.Errorf("probe process lock: %w", err)
	}
	return false, windows.UnlockFileEx(h, 0, 1, 0, &overlap)
}
