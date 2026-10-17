// Package buildinfo contains version metadata injected into release binaries.
package buildinfo

import (
	"fmt"
	"runtime"
)

var (
	Distribution = "standalone"
	Version      = "dev"
	Commit       = "unknown"
	BuiltAt      = "unknown"
)

func String() string {
	return fmt.Sprintf("wasync %s (%s/%s; distribution %s; commit %s; built %s)", Version, runtime.GOOS, runtime.GOARCH, Distribution, Commit, BuiltAt)
}
