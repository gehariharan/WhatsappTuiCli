package whatsapp

import (
	"runtime"

	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"google.golang.org/protobuf/proto"
)

// Pairing properties are process-wide upstream. Set these once before any
// clients can connect, without altering the WhatsApp protocol version.
func init() {
	store.DeviceProps.Os = proto.String(deviceName(runtime.GOOS))
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
}

func deviceName(platform string) string {
	switch platform {
	case "darwin":
		return "wasync (macOS)"
	case "windows":
		return "wasync (Windows)"
	case "linux":
		return "wasync (Linux)"
	default:
		return "wasync (" + platform + ")"
	}
}
