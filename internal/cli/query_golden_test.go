package cli

import (
	"context"
	"testing"

	"github.com/gehariharan/wasync/internal/golden"
)

// TestQuerySnapshots freezes the agent-facing wasync query and help --json
// output. Additive interfaces must use new commands or flags, never these shapes.
func TestQuerySnapshots(t *testing.T) {
	a, out, _, _, dir := queryFixture(t)
	snapshots := make([]any, 0)
	record := func(args ...string) {
		out.Reset()
		err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, args...))
		entry := map[string]any{"args": args}
		if err != nil {
			entry["error"] = err.Error()
			entry["stdout_bytes"] = out.Len()
		} else {
			entry["stdout"] = golden.Redact(golden.Decode(t, out.Bytes()), "received_at", "last_received_at", "last_discovery_at", "last_history_sync_at", "last_capture_heartbeat_at", "last_capture_finished_at")
		}
		snapshots = append(snapshots, entry)
	}
	record("groups")
	record("groups", "--enabled")
	record("chats", "--json")
	record("status")
	record("messages", "--chat", "one@g.us")
	record("messages", "--group", "one@g.us", "--limit", "2")
	record("messages", "--chat", "one@g.us", "--limit", "2", "--cursor", "eyJ2IjoxLCJiZWZvcmUiOnsiVGltZXN0YW1wIjoxNzU5Mjc2ODAwMDAwLCJJRCI6Mn0sInNjb3BlIjoiYmFkIn0")
	record("search", "hello")
	record("search", "--chat", "one@g.us", "--sender", "sender@lid", "--since", "2026-10-01T00:00:00Z", "--until", "2026-10-02T00:00:00Z", "archive")
	record("search", "no matches")
	record("messages")
	record("messages", "--chat", "missing@g.us")
	record("messages", "--chat", "one@g.us", "--limit", "201")
	record("messages", "--chat", "one@g.us", "--since", "bad")
	record("unknown")
	golden.Compare(t, "query.json", snapshots)

	out.Reset()
	if err := a.Run(context.Background(), []string{"help", "--json"}); err != nil {
		t.Fatal(err)
	}
	golden.Compare(t, "help.json", golden.Decode(t, out.Bytes()))
}
