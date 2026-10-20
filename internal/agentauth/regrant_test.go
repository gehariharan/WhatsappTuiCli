package agentauth

import (
	"strings"
	"testing"
	"time"
)

func TestExpiringRegrantRequiresHumanExpiryDecision(t *testing.T) {
	expires := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	client := &Client{Name: "reader", Scopes: []string{ScopeReadChats}, Chats: []string{"family@g.us"}, RatePerMinute: 5, ExpiresAt: &expires}
	for _, help := range []string{RegrantCommand(client, ScopeObserve), regrantCommand(client, ScopeReadMessages, "", 0), regrantCommand(client, "", "other@g.us", 0), regrantCommand(client, "", "", 6)} {
		if strings.Contains(help, "wasync agents grant") || strings.Contains(help, "wasync agents revoke") {
			t.Fatalf("unsafe copyable replacement command: %s", help)
		}
		if !strings.Contains(help, expires.Format(time.RFC3339)) || !strings.Contains(help, "explicitly choose an expiry") {
			t.Fatalf("missing expiry decision: %s", help)
		}
	}
}
