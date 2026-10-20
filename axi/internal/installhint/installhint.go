// Package installhint provides platform-specific commands for the local core.
package installhint

import "runtime"

const tap = "brew tap --custom-remote gehariharan/tap https://github.com/gehariharan/wasync.git && "

// CoreCommand describes a supported installation or upgrade on goos.
func CoreCommand(goos, operation string) string {
	switch goos {
	case "darwin":
		return tap + "brew " + operation + " --cask gehariharan/tap/wasync"
	case "linux":
		return tap + "brew " + operation + " --formula gehariharan/tap/wasync"
	default:
		return "download the matching wasync archive from https://github.com/gehariharan/wasync/releases"
	}
}

func InstallCore() string { return CoreCommand(runtime.GOOS, "install") }
func UpgradeCore() string { return CoreCommand(runtime.GOOS, "upgrade") }
