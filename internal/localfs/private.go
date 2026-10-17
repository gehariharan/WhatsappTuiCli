// Package localfs creates private application files without following symlinks.
package localfs

import (
	"os"
)

func EnsureFile(path string) error {
	f, err := OpenFile(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// CheckSidecars prevents SQLite from opening pre-existing symlink sidecars.
func CheckSidecars(path string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		name := path + suffix
		if _, err := os.Lstat(name); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := EnsureFile(name); err != nil {
			return err
		}
	}
	return nil
}
