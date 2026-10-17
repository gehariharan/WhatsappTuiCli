// Package tui provides the full-screen interface and a single shared sync session.
package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"whatsappsync/internal/storage"
	"whatsappsync/internal/whatsapp"
)

type Client interface {
	AccountJID() string
	LoginWithQR(context.Context, func(string)) error
	EnableCapture(context.Context, *storage.Store) error
	Groups(context.Context) ([]storage.Group, error)
	Status() whatsapp.CaptureStatus
	Maintain(context.Context) error
	Logout(context.Context) error
	StopCapture(error)
}
type Snapshot struct {
	APIURL  string
	Status  whatsapp.CaptureStatus
	Groups  []storage.Group
	Account int64
}
type Session struct {
	APIURL     string
	ctx        context.Context
	store      *storage.Store
	client     Client
	network    sync.Mutex
	maintained time.Time
}

func NewSession(ctx context.Context, s *storage.Store, c Client) *Session {
	return &Session{ctx: ctx, store: s, client: c}
}
func (s *Session) Connect(display func(string)) error {
	if s.client == nil {
		return fmt.Errorf("Offline archive browsing; capture is stopped")
	}
	if !s.network.TryLock() {
		return fmt.Errorf("a connection operation is already running")
	}
	defer s.network.Unlock()
	if err := s.client.LoginWithQR(s.ctx, display); err != nil {
		return err
	}
	return s.refresh()
}
func (s *Session) Refresh() error {
	if s.client == nil {
		return fmt.Errorf("offline mode: restart without --offline to connect")
	}
	if !s.network.TryLock() {
		return fmt.Errorf("connection is busy")
	}
	defer s.network.Unlock()
	if s.client.Status().Connection != "online" {
		return fmt.Errorf("not connected; press c to connect")
	}
	return s.refresh()
}
func (s *Session) refresh() error {
	groups, err := s.client.Groups(s.ctx)
	if err != nil {
		return err
	}
	account := s.client.Status().AccountID
	if account == 0 {
		return fmt.Errorf("authenticated account not ready")
	}
	return s.store.SaveGroups(s.ctx, account, groups)
}
func (s *Session) Snapshot() (Snapshot, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	snap := Snapshot{APIURL: s.APIURL, Status: whatsapp.CaptureStatus{Connection: "offline browsing"}}
	if s.client != nil {
		snap.Status = s.client.Status()
		if time.Since(s.maintained) > 10*time.Second {
			if err := s.client.Maintain(ctx); err != nil {
				s.client.StopCapture(err)
				return snap, err
			}
			s.maintained = time.Now()
		}
	}
	account, err := s.store.Account(ctx)
	if err != nil {
		return snap, err
	}
	if account != nil {
		snap.Account = account.ID
		snap.Groups, err = s.store.GroupSummaries(ctx, account.ID)
	}
	return snap, err
}
func (s *Session) Toggle(account int64, group string, enabled bool) error {
	return s.store.SetEnabled(s.ctx, account, []string{group}, enabled)
}
func (s *Session) Purge(account int64, group string) error {
	return s.store.PurgeGroup(s.ctx, account, group)
}
func (s *Session) Messages(account int64, group string, q storage.MessageQuery) (storage.MessagePage, error) {
	return s.store.Messages(s.ctx, account, group, q)
}
func (s *Session) Logout() error {
	if s.client == nil {
		return fmt.Errorf("offline mode cannot unlink a device")
	}
	if !s.network.TryLock() {
		return fmt.Errorf("connection is busy")
	}
	defer s.network.Unlock()
	if s.client.Status().Connection != "online" {
		return fmt.Errorf("connect before unlinking")
	}
	return s.client.Logout(s.ctx)
}

func (s *Session) RequestHistory(account int64, group string) error {
	return s.store.QueueHistory(s.ctx, account, group)
}
