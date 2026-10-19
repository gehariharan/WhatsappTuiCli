//go:build !windows

package localfs

import (
	"fmt"
	"os"
)

// CheckPrivateDir validates an existing data directory without creating or
// changing it: a real directory accessible only by its owner (0700).
func CheckPrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("data directory must be a real directory")
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("data directory must have permissions 0700")
	}
	return nil
}
