// Package buildinfo contains version metadata injected into release binaries.
package buildinfo

import (
	"fmt"
	"runtime"
)

var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

func String() string {
	return fmt.Sprintf("wasync %s (%s/%s; commit %s; built %s)", Version, runtime.GOOS, runtime.GOARCH, Commit, BuiltAt)
}
