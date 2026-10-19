package whatsapp

import (
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow/types/events"
)

// healthFacts are lock-free observations for deep health checks. They hold
// timestamps only, never event contents.
type healthFacts struct {
	lastEvent, lastConnected, lastKeepAliveOK atomic.Int64 // Unix nanoseconds; 0 = never
	keepAliveFailing                          atomic.Bool
	paired                                    atomic.Bool
}

// record notes when evt was observed. Locally generated failure signals
// (keepalive timeouts, disconnects) are not proof of contact with WhatsApp and
// do not advance the last-event time.
func (h *healthFacts) record(evt any, now time.Time) {
	switch e := evt.(type) {
	case *events.KeepAliveTimeout:
		h.keepAliveFailing.Store(true)
		if !e.LastSuccess.IsZero() {
			storeLater(&h.lastKeepAliveOK, e.LastSuccess)
		}
		return
	case *events.Disconnected:
		return
	case *events.Connected:
		h.lastConnected.Store(now.UnixNano())
		h.lastKeepAliveOK.Store(now.UnixNano())
		h.keepAliveFailing.Store(false)
		h.paired.Store(true)
	case *events.KeepAliveRestored:
		h.lastKeepAliveOK.Store(now.UnixNano())
		h.keepAliveFailing.Store(false)
	case *events.PairSuccess:
		h.paired.Store(true)
	case *events.LoggedOut:
		h.paired.Store(false)
	}
	h.lastEvent.Store(now.UnixNano())
}

func storeLater(v *atomic.Int64, t time.Time) {
	n := t.UnixNano()
	for {
		old := v.Load()
		if old >= n || v.CompareAndSwap(old, n) {
			return
		}
	}
}

func loadTime(v *atomic.Int64) time.Time {
	if n := v.Load(); n != 0 {
		return time.Unix(0, n).UTC()
	}
	return time.Time{}
}

func (h *healthFacts) fill(s *CaptureStatus) {
	s.LastEventAt = loadTime(&h.lastEvent)
	s.LastConnectedAt = loadTime(&h.lastConnected)
	s.LastKeepAliveOK = loadTime(&h.lastKeepAliveOK)
	s.KeepAliveFailing = h.keepAliveFailing.Load()
	s.Paired = h.paired.Load()
}
