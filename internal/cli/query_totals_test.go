package cli

import (
	"encoding/json"
	"testing"

	"github.com/gehariharan/wasync/internal/query"
)

func TestOfflineSearchTotalsMatchAcrossPages(t *testing.T) {
	a, out, writer, account, dir := queryFixture(t)
	seedV1(t, writer, account)
	var first struct {
		Data query.V1MessageList `json:"data"`
		Page query.V1Page        `json:"page"`
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "search", "hello", "--limit", "1", "--v1"), &first); err != nil {
		t.Fatal(err)
	}
	if first.Data.Total == nil || *first.Data.Total != 4 || !first.Page.HasMore {
		t.Fatalf("first page %+v", first)
	}
	var next struct {
		Data query.V1MessageList `json:"data"`
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "search", "hello", "--limit", "1", "--cursor", first.Page.NextCursor, "--v1"), &next); err != nil {
		t.Fatal(err)
	}
	if next.Data.Total == nil || *next.Data.Total != 4 {
		t.Fatalf("next page %+v", next)
	}
}
