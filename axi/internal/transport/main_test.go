package transport

import (
	"os"
	"testing"

	"github.com/gehariharan/whatsapp-axi/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.MaybeRunFakeWasync()
	os.Exit(m.Run())
}
