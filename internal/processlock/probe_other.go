//go:build !darwin && !linux && !windows

package processlock

import "fmt"

// Held is unsupported on this platform.
func Held(string) (bool, error) { return false, fmt.Errorf("Phase 1 supports macOS and Linux only") }
