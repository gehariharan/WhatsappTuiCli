package api

import (
	"net/url"
	"testing"

	"github.com/gehariharan/wasync/internal/agentauth"
	"github.com/gehariharan/wasync/internal/query"
)

func TestV1TotalsRespectFiltersPermissionsAndPagination(t *testing.T) {
	f := setup(t)
	seedContext(t, f.store, f.account)
	limited := f.grant(t, "totals-reader", []string{agentauth.ScopeReadMessages}, "ctx@g.us")
	cases := []struct {
		path, token string
		total       int64
	}{
		{"/api/v1/messages/search?q=needle&limit=1", f.agent, 2},
		{"/api/v1/messages/search?q=needle&limit=1", limited, 1},
		{"/api/v1/messages/search?q=needle&chat=ctx@g.us&limit=1", f.agent, 1},
		{"/api/v1/messages/search?q=absent", f.agent, 0},
		{"/api/v1/messages/search?q=needle&sender_id=absent", f.agent, 0},
		{"/api/v1/chats/ctx@g.us/messages?limit=1", f.agent, 7},
	}
	for _, c := range cases {
		body := f.get(t, c.path, c.token, 200)
		var data struct {
			Total *int64            `json:"total"`
			Items []query.V1Message `json:"items"`
		}
		data = decodeInto[struct {
			Total *int64            `json:"total"`
			Items []query.V1Message `json:"items"`
		}](t, body.Data)
		if data.Total == nil || *data.Total != c.total {
			t.Fatalf("%s total=%v want=%d", c.path, data.Total, c.total)
		}
		if body.Page != nil && body.Page.HasMore {
			next := f.get(t, c.path+"&cursor="+url.QueryEscape(body.Page.NextCursor), c.token, 200)
			nextData := decodeInto[struct {
				Total *int64 `json:"total"`
			}](t, next.Data)
			if nextData.Total == nil || *nextData.Total != c.total {
				t.Fatalf("cursor changed total: %s", next.Data)
			}
		}
	}
}
