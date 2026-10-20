package installhint

import "testing"

func TestCoreCommandUsesSupportedPlatformPackage(t *testing.T) {
	for _, tc := range []struct{ os, operation, want string }{
		{"linux", "install", tap + "brew install --formula gehariharan/tap/wasync"},
		{"linux", "upgrade", tap + "brew upgrade --formula gehariharan/tap/wasync"},
		{"darwin", "install", tap + "brew install --cask gehariharan/tap/wasync"},
		{"darwin", "upgrade", tap + "brew upgrade --cask gehariharan/tap/wasync"},
		{"windows", "install", "download the matching wasync archive from https://github.com/gehariharan/wasync/releases"},
	} {
		if got := CoreCommand(tc.os, tc.operation); got != tc.want {
			t.Errorf("%s %s: got %q; want %q", tc.os, tc.operation, got, tc.want)
		}
	}
}
