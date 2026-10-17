//go:build windows

package windowscheck

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"whatsappsync/internal/localfs"
	"whatsappsync/internal/processlock"
	"whatsappsync/internal/storage"
)

func TestPrivateWindowsArchiveLockAndFTS(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := localfs.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "device.lock")
	lock, err := processlock.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	other, err := processlock.Acquire(lockPath)
	if other != nil {
		other.Close()
		t.Fatal("second process owner lock accepted")
	}
	if !errors.Is(err, processlock.ErrBusy) {
		t.Fatalf("expected busy: %v", err)
	}
	ctx := context.Background()
	path := filepath.Join(dir, "archive.db")
	store, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.UseAccount(ctx, "test@lid")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveDiscoveredChat(ctx, account, storage.Group{ID: "555@lid", Name: "Synthetic"}); err != nil {
		t.Fatal(err)
	}
	if err = store.SetEnabled(ctx, account, []string{"555@lid"}, true); err != nil {
		t.Fatal(err)
	}
	_, err = store.Apply(ctx, account, []storage.Message{{GroupID: "555@lid", MessageID: "one", Timestamp: time.Now(), Text: "synthetic searchable", Type: "text", Source: "live"}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := storage.OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	page, err := reader.Messages(ctx, account, "555@lid", storage.MessageQuery{Search: "searchable", Limit: 5})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("FTS/WAL read failed: %+v %v", page, err)
	}
}
