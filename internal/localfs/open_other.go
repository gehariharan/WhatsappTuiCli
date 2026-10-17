//go:build !darwin && !linux && !windows

package localfs

import (
	"fmt"
	"os"
)

func openNoFollow(string) (*os.File, error) {
	return nil, fmt.Errorf("Phase 1 supports macOS and Linux only")
}
