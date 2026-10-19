package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"whatsappsync/internal/changecursor"
	"whatsappsync/internal/query"
)

func queryV1Err(t *testing.T, a *App, out *bytes.Buffer, dir, code string, args ...string) query.V1Error {
	t.Helper()
	out.Reset()
	err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, args...))
	var e query.V1Error
	if !errors.As(err, new(*ReportedError)) || json.Unmarshal(out.Bytes(), &e) != nil || e.ErrorCode != code || e.Error != err.Error() || !cliRequestID.MatchString(e.RequestID) {
		t.Fatalf("%v: %v %s", args, err, out.Bytes())
	}
	return e
}

func TestQueryChangesOffline(t *testing.T) {
	a, out, writer, account, dir := queryFixture(t)
	var all struct {
		Data query.V1Changes `json:"data"`
		Page query.V1Page    `json:"page"`
	}
	// Always a v1 envelope, with or without --v1.
	for _, args := range [][]string{{"changes"}, {"changes", "--v1"}, {"changes", "--json"}} {
		if err := json.Unmarshal(queryRun(t, a, out, dir, args...), &all); err != nil {
			t.Fatal(err)
		}
		if len(all.Data.Changes) != 4 || all.Data.Changes[0].Op != "capture.enabled" || all.Data.Changes[3].Op != "message.created" || all.Data.Changes[3].Ref == "" || all.Page.HasMore || all.Page.NextCursor != all.Data.Changes[3].Cursor {
			t.Fatalf("%v: %+v", args, all)
		}
	}
	var page struct {
		Data query.V1Changes `json:"data"`
		Page query.V1Page    `json:"page"`
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "changes", "--limit", "3"), &page); err != nil || len(page.Data.Changes) != 3 || !page.Page.HasMore {
		t.Fatalf("first page %+v %v", page, err)
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "changes", "--after", page.Page.NextCursor), &page); err != nil || len(page.Data.Changes) != 1 || page.Page.HasMore || page.Page.NextCursor != all.Page.NextCursor {
		t.Fatalf("second page %+v %v", page, err)
	}
	if e := queryV1Err(t, a, out, dir, query.CodeInvalidCursor, "changes", "--after", "garbage"); e.Help == "" {
		t.Fatal("no help")
	}
	epoch, err := writer.JournalEpoch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []changecursor.Cursor{{Account: "test@lid", Epoch: strings.Repeat("a", 32), Sequence: 1}, {Account: "other@lid", Epoch: epoch, Sequence: 1}} {
		if e := queryV1Err(t, a, out, dir, query.CodeCursorExpired, "changes", "--after", changecursor.Encode(c)); !strings.Contains(e.Help, "Resnapshot") {
			t.Fatalf("help %q", e.Help)
		}
	}
	for _, args := range [][]string{{"changes", "--limit", "0"}, {"changes", "--limit", "1001"}, {"changes", "extra"}, {"changes", "--consumer", "x"}, {"changes", "--json=false"}} {
		queryV1Err(t, a, out, dir, query.CodeInvalidArgument, args...)
	}

	// Checkpoints are read only offline.
	var cp struct {
		Data query.V1Checkpoint `json:"data"`
	}
	if err = json.Unmarshal(queryRun(t, a, out, dir, "checkpoint", "--consumer", "replica", "--v1"), &cp); err != nil || cp.Data.Consumer != "replica" || cp.Data.Cursor != nil || cp.Data.UpdatedAt != nil {
		t.Fatalf("unset checkpoint %s %v", out.Bytes(), err)
	}
	for _, args := range [][]string{
		{"checkpoint", "--consumer", "replica", "--cursor", all.Page.NextCursor},
		{"checkpoint", "--consumer", "replica", "--set", all.Page.NextCursor},
		{"checkpoint", "--consumer", "replica", all.Page.NextCursor},
		{"checkpoint"},
		{"checkpoint", "--consumer", "Bad_Name"},
	} {
		queryV1Err(t, a, out, dir, query.CodeInvalidArgument, args...)
	}
	if _, found, err := writer.Checkpoint(context.Background(), account, "replica"); err != nil || found {
		t.Fatalf("offline query wrote a checkpoint: %v %v", found, err)
	}
	if _, err = writer.SetCheckpoint(context.Background(), account, "replica", epoch, all.Data.Changes[1].Sequence); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(queryRun(t, a, out, dir, "checkpoint", "--consumer", "replica"), &cp); err != nil || cp.Data.Cursor == nil || *cp.Data.Cursor != all.Data.Changes[1].Cursor || cp.Data.UpdatedAt == nil {
		t.Fatalf("checkpoint %s %v", out.Bytes(), err)
	}

	for _, command := range []string{"changes", "checkpoint"} {
		out.Reset()
		if err = a.Run(context.Background(), []string{"--data-dir", dir, "query", command, "--help"}); err != nil || !bytes.Contains(out.Bytes(), []byte("Usage: wasync [--data-dir PATH] query "+command)) || !bytes.Contains(out.Bytes(), []byte("PUT /api/v1/checkpoints")) {
			t.Fatalf("%s help: %v %s", command, err, out.Bytes())
		}
	}
}
