package whatsapp

import (
	"bytes"
	"context"
	"errors"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestUnpairedSessionReopensWithoutNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	for range 2 {
		c, err := Open(ctx, path, log)
		if err != nil {
			t.Fatal(err)
		}
		if c.Paired() {
			t.Fatal("fresh store appears paired")
		}
		if err = c.Connect(ctx); !errors.Is(err, ErrNotPaired) {
			t.Fatalf("unpaired connect: %v", err)
		}
		if err = c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestLifecycleAndObservationDiscardPayloads(t *testing.T) {
	c := &Client{connected: make(chan struct{}, 1), fatal: make(chan error, 1), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.onEvent(&events.Connected{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.waitAuthenticated(ctx); err != nil {
		t.Fatal(err)
	}
	c.onEvent(&events.Message{})
	c.onEvent(&events.HistorySync{})
	if o := c.Observation(); o.LiveEvents != 1 || o.HistoryBatches != 1 {
		t.Fatalf("bad observation %+v", o)
	}
	c.onEvent(&events.LoggedOut{})
	if err := c.Observe(ctx, time.Second); err == nil {
		t.Fatal("terminal disconnect ignored")
	}
}

func TestSavedDeviceCredentialsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "session.db")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := Open(ctx, path, log)
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic local fixture only: this does not pair with or contact WhatsApp.
	device := c.client.Store
	jid, err := types.ParseJID("15550000000:1@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	device.ID = &jid
	device.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte("fixture"), AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err = device.Save(ctx); err != nil {
		t.Fatal(err)
	}
	noise := append([]byte(nil), device.NoiseKey.Priv[:]...)
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = Open(ctx, path, log)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.Paired() || c.AccountJID() != "15550000000@s.whatsapp.net" {
		t.Fatal("saved device identity did not survive restart")
	}
	if !bytes.Equal(noise, c.client.Store.NoiseKey.Priv[:]) {
		t.Fatal("device keys were regenerated")
	}
}
