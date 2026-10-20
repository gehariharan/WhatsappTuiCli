package transport

import (
	"os"
	"testing"

	"github.com/gehariharan/wasync/axi/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.MaybeRunFakeWasync()
	os.Exit(m.Run())
}
