//go:build !windows

package localfs

import (
	"fmt"
	"os"
)

func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("create private directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("data directory must be a real directory")
	}
	// Refuse to change permissions on a possibly unrelated, pre-existing directory.
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("data directory must have permissions 0700; use a dedicated private directory")
	}
	return nil
}

func OpenFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, fmt.Errorf("refusing non-regular private file %s", path)
	}
	f, err := openNoFollow(path)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func CheckPrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("archive and sidecars must be regular private files (0600)")
	}
	return nil
}
