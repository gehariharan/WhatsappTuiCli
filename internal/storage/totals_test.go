package storage

import (
	"context"
	"testing"
	"time"
)

func TestCountMessagesUsesReadFiltersWithoutCursor(t *testing.T) {
	s, account := captureStore(t)
	ctx := context.Background()
	for i, text := range []string{"needle one", "needle two", "other"} {
		m := original(string(rune('a'+i)), text)
		m.Timestamp = time.Unix(1700000000+int64(i), 0)
		mustApply(t, s, account, m)
	}
	from, until := time.Unix(1700000001, 0), time.Unix(1700000002, 0)
	for _, c := range []struct {
		query MessageQuery
		chats []string
		want  int64
	}{
		{MessageQuery{Search: "needle", Limit: 1, Before: Cursor{1, 1}}, nil, 2},
		{MessageQuery{Search: "needle", From: &from, To: &until}, nil, 1},
		{MessageQuery{Sender: "absent"}, nil, 0},
		{MessageQuery{}, []string{"two@g.us"}, 0},
		{MessageQuery{}, []string{"one@g.us"}, 3},
	} {
		total, err := s.CountMessages(ctx, account, "", c.query, c.chats)
		if err != nil || total != c.want {
			t.Fatalf("%+v: total=%d want=%d err=%v", c, total, c.want, err)
		}
	}
	if total, err := s.CountMessages(ctx, account+1, "", MessageQuery{}, nil); err != nil || total != 0 {
		t.Fatalf("account isolation: %d %v", total, err)
	}
}
