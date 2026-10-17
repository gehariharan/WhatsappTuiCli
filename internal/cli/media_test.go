package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"whatsappsync/internal/api"
	"whatsappsync/internal/whatsapp"
)

func TestMediaCommandsReuseOwnerAndQueriesNeverDownload(t *testing.T) {
	app, out, store, account, dir := queryFixture(t)
	var downloads atomic.Int32
	server, err := api.StartMediaControlled("127.0.0.1:0", dir, store, nil, nil, nil, nil, func(_ context.Context, chat, message string) (whatsapp.MediaResult, error) {
		downloads.Add(1)
		return whatsapp.MediaResult{ChatID: chat, MessageID: message, Status: "queued"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	flags := []string{"--data-dir", dir, "--api-addr", strings.TrimPrefix(server.URL, "http://")}
	if err = app.Run(context.Background(), append(flags, "media", "policy", "--chat", "one@g.us", "--mode", "manual")); err != nil {
		t.Fatal(err)
	}
	mode, err := store.MediaPolicy(context.Background(), account, "one@g.us")
	if err != nil || mode != "manual" {
		t.Fatal(mode, err)
	}
	out.Reset()
	if err = app.Run(context.Background(), append(flags, "media", "download", "--chat", "one@g.us", "--message-id", "0")); err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 1 {
		t.Fatal("explicit request not routed to owner")
	}
	queryRun(t, app, out, dir, "messages", "--chat", "one@g.us")
	if downloads.Load() != 1 {
		t.Fatal("query triggered a download")
	}
	readToken, err := os.ReadFile(filepath.Join(dir, "api.token"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"policy", "download"} {
		req, _ := http.NewRequest("POST", server.URL+"/api/media/"+path, bytes.NewBufferString(`{"chat_id":"one@g.us","message_id":"0"}`))
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(readToken)))
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatal("read credential allowed media control", path, response.StatusCode)
		}
	}
	if downloads.Load() != 1 {
		t.Fatal("unauthorized request downloaded media")
	}
}
