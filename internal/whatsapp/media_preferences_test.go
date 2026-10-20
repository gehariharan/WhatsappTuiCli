package whatsapp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gehariharan/wasync/internal/localfs"
	"github.com/gehariharan/wasync/internal/storage"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestManualMediaReferenceSurvivesRestartWithoutChangingPolicy(t *testing.T) {
	ctx := context.Background()
	dir := privateTestDir(t)
	path := filepath.Join(dir, "archive.db")
	store, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.UseAccount(ctx, "synthetic@lid")
	if err != nil {
		t.Fatal(err)
	}
	chat := "123@g.us"
	if err = store.SaveGroups(ctx, account, []storage.Group{{ID: chat, Name: "Selected"}}); err != nil {
		t.Fatal(err)
	}
	if err = store.SetEnabled(ctx, account, []string{chat}, true); err != nil {
		t.Fatal(err)
	}
	if err = store.SetMediaPolicy(ctx, account, chat, "manual"); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("abc"))
	event := fixture(&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(3), FileSHA256: hash[:], MediaKey: []byte("private-reference-key"), DirectPath: proto.String("/private-download-reference")}})
	message, ok := Normalize(event, "live", time.Now())
	if !ok {
		t.Fatal("not normalized")
	}
	if _, err = store.Apply(ctx, account, []storage.Message{message}); err != nil {
		t.Fatal(err)
	}
	client := &Client{capture: &capture{ctx: ctx, store: store}, mediaRoot: filepath.Join(dir, "media"), mediaEnabled: true, mediaQueue: make(chan mediaJob, 4), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	client.capture.account.Store(account)
	if err = client.queueMedia(account, event, message); err != nil {
		t.Fatal(err)
	}
	if len(client.mediaQueue) != 0 {
		t.Fatal("manual policy failed to override global flag")
	}
	refPath := referencePath(ReferenceRoot(client.mediaRoot), account, chat, message.MessageID)
	if err = localfs.CheckPrivateFile(refPath); err != nil {
		t.Fatal("reference is not private", err)
	}
	current, err := store.MessageByWhatsAppID(ctx, account, chat, message.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(current)
	if strings.Contains(string(encoded), "private-reference-key") || strings.Contains(string(encoded), "private-download-reference") {
		t.Fatal("query metadata leaked credentials")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// New client with only the persisted archive and reference files.
	restarted := &Client{capture: &capture{ctx: ctx, store: store}, mediaRoot: client.mediaRoot, mediaQueue: make(chan mediaJob, 4), mediaConnected: func() bool { return true }, log: client.log}
	restarted.capture.account.Store(account)
	downloads := 0
	restarted.mediaTransfer = func(_ context.Context, _ whatsmeow.DownloadableMessage, file whatsmeow.File) error {
		downloads++
		_, err := file.Write([]byte("abc"))
		return err
	}
	result, err := restarted.RequestMedia(ctx, chat, message.MessageID)
	if err != nil || result.Status != "queued" {
		t.Fatal(result, err)
	}
	if _, err = restarted.RequestMedia(ctx, chat, message.MessageID); err != nil || len(restarted.mediaQueue) != 1 {
		t.Fatal("duplicate manual request queued", err)
	}
	job := <-restarted.mediaQueue
	if !job.manual {
		t.Fatal("one-time request was not marked explicit")
	}
	if err = restarted.downloadMedia(ctx, job); err != nil {
		t.Fatal(err)
	}
	restarted.finishMedia(job)
	result, err = restarted.RequestMedia(ctx, chat, message.MessageID)
	if err != nil || result.Status != "already_downloaded" || downloads != 1 {
		t.Fatal(result, err, downloads)
	}
	// A history replay while the queue is full must preserve a cached file.
	before, err := store.MessageByWhatsAppID(ctx, account, chat, message.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cap(restarted.mediaQueue); i++ {
		restarted.mediaQueue <- mediaJob{}
	}
	if err = restarted.enqueueMedia(ctx, job); err != nil {
		t.Fatal("cached file rejected with a full queue", err)
	}
	after, err := store.MessageByWhatsAppID(ctx, account, chat, message.MessageID)
	if err != nil || after.Media != before.Media {
		t.Fatal("cached file metadata was overwritten", err)
	}
	for len(restarted.mediaQueue) > 0 {
		<-restarted.mediaQueue
	}
	policy, err := store.MediaPolicy(ctx, account, chat)
	if err != nil || policy != "manual" {
		t.Fatal("manual download changed chat policy", policy, err)
	}
	if err = store.SetEnabled(ctx, account, []string{chat}, false); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.RequestMedia(ctx, chat, message.MessageID); err == nil {
		t.Fatal("download permitted disabled capture")
	}
	// A corrupt managed reference sorting before the revoked one cannot block cleanup.
	bad := filepath.Join(filepath.Dir(refPath), strings.Repeat("0", 64)+".ref")
	if err = os.WriteFile(bad, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.ClearMessage(ctx, account, chat, message.MessageID); err != nil {
		t.Fatal(err)
	}
	restarted.sweepMedia(ctx)
	if _, e := os.Stat(bad); !os.IsNotExist(e) {
		t.Fatal("corrupt reference retained", e)
	}
	if _, err = os.Stat(refPath); !os.IsNotExist(err) {
		t.Fatal("deleted attachment credential retained", err)
	}
}

func TestMediaPolicyPersistsAndOverridesGlobalSetting(t *testing.T) {
	ctx := context.Background()
	dir := privateTestDir(t)
	path := filepath.Join(dir, "archive.db")
	store, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.UseAccount(ctx, "synthetic@lid")
	if err != nil {
		t.Fatal(err)
	}
	chat := "one@g.us"
	if err = store.SaveGroups(ctx, account, []storage.Group{{ID: chat, Name: "One"}}); err != nil {
		t.Fatal(err)
	}
	c := &Client{capture: &capture{ctx: ctx, store: store}}
	for _, mode := range []string{"inherit", "manual", "auto"} {
		if err = store.SetMediaPolicy(ctx, account, chat, mode); err != nil {
			t.Fatal(err)
		}
		for _, global := range []bool{false, true} {
			c.mediaEnabled = global
			got, e := c.autoMedia(ctx, account, chat)
			want := mode == "auto" || (mode == "inherit" && global)
			if e != nil || got != want {
				t.Fatal(mode, global, got, e)
			}
		}
	}
	if err = store.SetMediaPolicy(ctx, account, chat, "invalid"); err == nil {
		t.Fatal("invalid policy accepted")
	}
	groups, err := store.GroupSummaries(ctx, account)
	if err != nil || len(groups) != 1 || groups[0].MediaPolicy != "auto" {
		t.Fatal(groups, err)
	}
}
