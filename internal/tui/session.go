// Package tui provides the full-screen interface and a single shared sync session.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"whatsappsync/internal/mediafiles"

	"whatsappsync/internal/approval"
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
	Pending *approval.Request
	APIURL  string
	Status  whatsapp.CaptureStatus
	Groups  []storage.Group
	Account int64
}
type Session struct {
	Approvals  *approval.Queue
	APIURL     string
	MediaRoot  string
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
	if s.client != nil && (account == nil || snap.Status.AccountID == 0 || snap.Status.AccountID != account.ID || snap.Status.Connection == "pairing") {
		return snap, nil
	}
	if account != nil {
		snap.Account = account.ID
		snap.Pending = s.Approvals.Pending(account.ID)
		snap.Groups, err = s.store.GroupSummaries(ctx, account.ID)
	}
	return snap, err
}
func (s *Session) Toggle(account int64, group string, enabled bool) error {
	return s.store.SetEnabled(s.ctx, account, []string{group}, enabled)
}
func (s *Session) Purge(account int64, group string) error {
	if err := s.store.PurgeGroup(s.ctx, account, group); err != nil {
		return err
	}
	if s.MediaRoot != "" {
		return os.RemoveAll(mediafiles.Directory(s.MediaRoot, account, group))
	}
	return nil
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

// ToggleMany applies the exact group set shown at confirmation time atomically.
func (s *Session) ToggleMany(account int64, ids []string, enabled bool) error {
	return s.store.SetEnabled(s.ctx, account, ids, enabled)
}

func (s *Session) ClearMessage(account int64, chat, message string) error {
	if err := s.store.ClearMessage(s.ctx, account, chat, message); err != nil {
		return err
	}
	return mediafiles.RemoveMessage(s.MediaRoot, account, chat, message)
}

func (s *Session) RevokeMessage(account int64, chat, message string) error {
	if s.client == nil {
		return fmt.Errorf("connect before remote deletion")
	}
	if !s.network.TryLock() {
		return fmt.Errorf("connection is busy")
	}
	defer s.network.Unlock()
	return s.revokeMessage(account, chat, message)
}
func (s *Session) revokeMessage(account int64, chat, message string) error {
	m, err := s.store.MessageByWhatsAppID(s.ctx, account, chat, message)
	if err != nil {
		return err
	}
	remote, ok := s.client.(interface {
		RevokeOwnMessage(context.Context, string, storage.Message) error
	})
	if !ok {
		return fmt.Errorf("remote revocation unavailable")
	}
	if err = remote.RevokeOwnMessage(s.ctx, chat, m); err != nil {
		return err
	}
	if err = s.ClearMessage(account, chat, message); err != nil {
		return &partialActionError{err}
	}
	return nil
}

type partialActionError struct{ err error }

func (e *partialActionError) Error() string {
	return "WhatsApp revocation acknowledged; local cleanup failed: " + e.err.Error()
}
func (s *Session) DeleteWhatsAppChat(account int64, chat string) error {
	if s.client == nil {
		return fmt.Errorf("connect before remote deletion")
	}
	if !s.network.TryLock() {
		return fmt.Errorf("connection is busy")
	}
	defer s.network.Unlock()
	return s.deleteWhatsAppChat(account, chat)
}
func (s *Session) deleteWhatsAppChat(account int64, chat string) error {
	page, err := s.store.Messages(s.ctx, account, chat, storage.MessageQuery{Limit: 1})
	if err != nil {
		return err
	}
	if len(page.Items) == 0 {
		return fmt.Errorf("known local message required to bound WhatsApp deletion")
	}
	remote, ok := s.client.(interface {
		DeleteWhatsAppChat(context.Context, string, storage.Message) error
	})
	if !ok {
		return fmt.Errorf("remote chat deletion unavailable")
	}
	return remote.DeleteWhatsAppChat(s.ctx, chat, page.Items[0])
}
func (s *Session) ApproveAction(account int64, id string) (result error) {
	if s.Approvals == nil {
		return fmt.Errorf("approval queue unavailable")
	}
	current, err := s.store.Account(s.ctx)
	if err != nil {
		return err
	}
	if current == nil || current.ID != account {
		return fmt.Errorf("account changed; request not executed")
	}
	pending := s.Approvals.Get(account, id)
	if pending == nil {
		return fmt.Errorf("request unavailable")
	}
	remote := pending.Kind == "revoke-message" || pending.Kind == "delete-whatsapp-chat"
	if remote {
		if s.client == nil || s.client.Status().Connection != "online" || s.client.Status().AccountID != account {
			return fmt.Errorf("connect the matching account before approval; request remains pending")
		}
		if !s.network.TryLock() {
			return fmt.Errorf("connection is busy; request remains pending")
		}
		defer s.network.Unlock()
	}
	request, err := s.Approvals.Take(account, id)
	if err != nil {
		return err
	}
	defer func() {
		status := "completed"
		if result != nil {
			status = "failed_or_unknown"
			var partial *partialActionError
			if errors.As(result, &partial) {
				status = "remote_completed_local_cleanup_failed"
			}
		}
		s.Approvals.Finish(id, status, result)
	}()
	switch request.Kind {
	case "clear-message":
		return s.ClearMessage(account, request.Chat, request.Message)
	case "purge-chat":
		return s.Purge(account, request.Chat)
	case "revoke-message":
		return s.revokeMessage(account, request.Chat, request.Message)
	case "delete-whatsapp-chat":
		return s.deleteWhatsAppChat(account, request.Chat)
	}
	return fmt.Errorf("unsupported action")
}
func (s *Session) RejectAction(account int64, id string) error {
	if s.Approvals == nil {
		return fmt.Errorf("approval queue unavailable")
	}
	if _, err := s.Approvals.Take(account, id); err != nil {
		return err
	}
	s.Approvals.Finish(id, "rejected", nil)
	return nil
}
