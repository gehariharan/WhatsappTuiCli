package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"whatsappsync/internal/approval"
	"whatsappsync/internal/localfs"
	"whatsappsync/internal/storage"
	"whatsappsync/internal/whatsapp"
)

func TestSendCredentialIsSeparateAndSignedRequestsCannotReplay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := localfs.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(dir, "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.UseAccount(ctx, "synthetic@lid")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveGroups(ctx, account, []storage.Group{{ID: "one@g.us", Name: "Synthetic"}}); err != nil {
		t.Fatal(err)
	}
	var sent atomic.Int32
	queue := &approval.Queue{}
	server, err := StartControlled("127.0.0.1:0", dir, store, nil, queue, func(account int64, id string) error { _, err := queue.Take(account, id); return err }, nil, func(context.Context, whatsapp.SendInput) (whatsapp.SendResult, error) {
		sent.Add(1)
		return whatsapp.SendResult{Status: "server_acknowledged"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	readToken, err := loadToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	controlToken, err := loadNamedToken(dir, "send.token")
	if err != nil {
		t.Fatal(err)
	}
	post := func(path string, body []byte, sign bool) int {
		t.Helper()
		req, err := http.NewRequest("POST", server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if sign {
			stamp := strconv.FormatInt(time.Now().Unix(), 10)
			nonce := "00000000000000000000000000000001"
			req.Header.Set("X-Wasync-Time", stamp)
			req.Header.Set("X-Wasync-Nonce", nonce)
			req.Header.Set("X-Wasync-Signature", SignCommand(controlToken, stamp, nonce, req.Method, path, body))
		} else {
			req.Header.Set("Authorization", "Bearer "+readToken)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		io.Copy(io.Discard, response.Body)
		return response.StatusCode
	}
	body := []byte(`{"to":"one@g.us","text":"synthetic"}`)
	if code := post("/api/send", body, false); code != 401 {
		t.Fatalf("query token gained send rights: %d", code)
	}
	if code := post("/api/send", body, true); code != 200 {
		t.Fatalf("signed send: %d", code)
	}
	if code := post("/api/send", body, true); code != 409 {
		t.Fatalf("replayed send: %d", code)
	}
	if sent.Load() != 1 {
		t.Fatal("replayed or unauthorized request sent a message")
	}
	// The sending credential can enqueue, but cannot execute an approval.
	requestBody, _ := json.Marshal(approval.Input{Kind: "purge-chat", Chat: "one@g.us"})
	sign := func(req *http.Request, token string, body []byte) {
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonceBytes := make([]byte, 16)
		rand.Read(nonceBytes)
		nonce := hex.EncodeToString(nonceBytes)
		req.Header.Set("X-Wasync-Time", stamp)
		req.Header.Set("X-Wasync-Nonce", nonce)
		req.Header.Set("X-Wasync-Signature", SignCommand(token, stamp, nonce, req.Method, req.URL.Path, body))
	}
	req, _ := http.NewRequest("POST", server.URL+"/api/actions", bytes.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer "+controlToken)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 || queue.Pending(account) != nil {
		t.Fatal("plaintext bearer queued an action")
	}
	req, _ = http.NewRequest("POST", server.URL+"/api/actions", bytes.NewReader(requestBody))
	sign(req, controlToken, requestBody)
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 202 || queue.Pending(account) == nil {
		t.Fatal("approval request not queued")
	}
	pending := queue.Pending(account)
	req, _ = http.NewRequest("POST", server.URL+"/api/actions/"+pending.ID+"/approve", nil)
	sign(req, controlToken, nil)
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode == 200 || queue.Pending(account) == nil {
		t.Fatal("remote approval route allowed a destructive action")
	}
}
