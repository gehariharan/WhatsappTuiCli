package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
	"whatsappsync/internal/whatsapp"
)

type groupJSON = query.Group

func groupDTO(g storage.Group) groupJSON { return query.FormatGroup(g) }
func (h *handler) getStatus(w http.ResponseWriter, r *http.Request) {
	a, err := h.store.Account(r.Context())
	if err != nil {
		fail(w, 500, "archive unavailable")
		return
	}
	var account int64
	enabled := 0
	enabledChats := 0
	var count int64
	if a != nil {
		account = a.ID
		groups, err := h.store.GroupSummaries(r.Context(), account)
		if err != nil {
			fail(w, 500, "archive unavailable")
			return
		}
		for _, g := range groups {
			if g.Enabled {
				enabledChats++
				if storage.ChatKind(g.ID) == "group" {
					enabled++
				}
			}
			count += g.MessageCount
		}
	}
	connection := "offline"
	var stored, skipped int64
	history := false
	attention := false
	if h.status != nil {
		s := h.status()
		connection = s.Connection
		stored = s.Stored
		skipped = s.Skipped
		history = s.HistoryProcessing
		attention = s.Error != ""
	}
	writeJSON(w, 200, map[string]any{"connection": connection, "account_id": account, "enabled_groups": enabled, "enabled_chats": enabledChats, "archived_messages": count, "stored_this_run": stored, "skipped_this_run": skipped, "history_processing": history, "attention_required": attention, "history_completeness": "unknown"})
}
func (h *handler) groups(w http.ResponseWriter, r *http.Request) {
	a := h.account(w, r)
	if a == nil {
		return
	}
	groups, err := h.store.GroupSummaries(r.Context(), a.ID)
	if err != nil {
		fail(w, 500, "archive unavailable")
		return
	}
	items := make([]groupJSON, 0, len(groups))
	for _, g := range groups {
		if strings.HasPrefix(r.URL.Path, "/api/chats") || storage.ChatKind(g.ID) == "group" {
			items = append(items, groupDTO(g))
		}
	}
	writeJSON(w, 200, map[string]any{"account_id": a.ID, "items": items})
}
func (h *handler) findGroup(w http.ResponseWriter, r *http.Request, account int64) *storage.Group {
	groups, err := h.store.GroupSummaries(r.Context(), account)
	if err != nil {
		fail(w, 500, "archive unavailable")
		return nil
	}
	for _, g := range groups {
		if g.ID == r.PathValue("id") && (strings.HasPrefix(r.URL.Path, "/api/chats") || storage.ChatKind(g.ID) == "group") {
			return &g
		}
	}
	fail(w, 404, "group not found")
	return nil
}
func (h *handler) group(w http.ResponseWriter, r *http.Request) {
	a := h.account(w, r)
	if a == nil {
		return
	}
	g := h.findGroup(w, r, a.ID)
	if g == nil {
		return
	}
	writeJSON(w, 200, groupDTO(*g))
}
func (h *handler) enable(w http.ResponseWriter, r *http.Request) {
	a := h.account(w, r)
	if a == nil {
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/chats/") && strings.HasSuffix(r.URL.Path, "/enable") {
		jid, err := whatsapp.ParseChat(r.PathValue("id"))
		if err != nil {
			fail(w, 400, "invalid chat ID")
			return
		}
		if storage.ChatKind(jid.String()) == "direct" {
			if err = h.store.SaveDiscoveredChat(r.Context(), a.ID, storage.Group{ID: jid.String(), Name: jid.String()}); err != nil {
				fail(w, 500, "cannot cache chat")
				return
			}
		}
	}
	g := h.findGroup(w, r, a.ID)
	if g == nil {
		return
	}
	enabled := strings.HasSuffix(r.URL.Path, "/enable")
	if err := h.store.SetEnabled(r.Context(), a.ID, []string{g.ID}, enabled); err != nil {
		fail(w, 500, "cannot update capture preference")
		return
	}
	writeJSON(w, 200, map[string]any{"id": g.ID, "sync_enabled": enabled})
}
func (h *handler) history(w http.ResponseWriter, r *http.Request) {
	a := h.account(w, r)
	if a == nil {
		return
	}
	g := h.findGroup(w, r, a.ID)
	if g == nil {
		return
	}
	if !g.Enabled {
		fail(w, 409, "enable group capture first")
		return
	}
	if err := h.store.QueueHistory(r.Context(), a.ID, g.ID); err != nil {
		fail(w, 409, "history request unavailable or already active")
		return
	}
	writeJSON(w, 202, map[string]string{"status": "queued", "completeness": "unknown"})
}

type messageJSON = query.Message

func messageDTO(account int64, m storage.Message) messageJSON { return query.FormatMessage(account, m) }
func (h *handler) messages(w http.ResponseWriter, r *http.Request) {
	a := h.account(w, r)
	if a == nil {
		return
	}
	g := h.findGroup(w, r, a.ID)
	if g == nil {
		return
	}
	h.listMessages(w, r, a.ID, g.ID, "")
}
func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	a := h.account(w, r)
	if a == nil {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 1024 {
		fail(w, 400, "q must contain 1 to 1024 bytes")
		return
	}
	h.listMessages(w, r, a.ID, r.URL.Query().Get("group_id"), q)
}
func (h *handler) message(w http.ResponseWriter, r *http.Request) {
	a := h.account(w, r)
	if a == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, 400, "invalid local message id")
		return
	}
	page, err := h.store.Messages(r.Context(), a.ID, "", storage.MessageQuery{ID: id, Limit: 1})
	if err != nil {
		fail(w, 500, "archive unavailable")
		return
	}
	if len(page.Items) == 0 {
		fail(w, 404, "message not found")
		return
	}
	writeJSON(w, 200, messageDTO(a.ID, page.Items[0]))
}
func (h *handler) listMessages(w http.ResponseWriter, r *http.Request, account int64, group, search string) {
	values := r.URL.Query()
	q := storage.MessageQuery{Limit: 50, Search: search, Sender: values.Get("sender_id")}
	if v := values.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			fail(w, 400, "limit must be between 1 and 200")
			return
		}
		q.Limit = n
	}
	for name, target := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		if v := values.Get(name); v != "" {
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				fail(w, 400, name+" must be RFC3339")
				return
			}
			t = t.UTC()
			*target = &t
		}
	}
	if q.From != nil && q.To != nil && !q.From.Before(*q.To) {
		fail(w, 400, "from must be before to")
		return
	}
	// Scope the signed cursor to the account and filters, allowing page size changes.
	scope, _ := json.Marshal([]any{account, group, search, q.Sender, q.From, q.To})
	if cursor := values.Get("cursor"); cursor != "" {
		before, err := h.decodeCursor(cursor, scope)
		if err != nil {
			fail(w, 400, "invalid cursor or filters changed")
			return
		}
		q.Before = before
	}
	page, err := h.store.Messages(r.Context(), account, group, q)
	if err != nil {
		fail(w, 500, "archive query failed")
		return
	}
	items := make([]messageJSON, 0, len(page.Items))
	for _, m := range page.Items {
		items = append(items, messageDTO(account, m))
	}
	next := ""
	if page.Next.ID != 0 {
		next = h.encodeCursor(page.Next, scope)
	}
	writeJSON(w, 200, map[string]any{"account_id": account, "items": items, "next_cursor": next})
}
func (h *handler) cursorMAC(payload, scope []byte) []byte {
	mac := hmac.New(sha256.New, []byte(h.token))
	mac.Write(scope)
	mac.Write([]byte{0})
	mac.Write(payload)
	return mac.Sum(nil)
}
func (h *handler) encodeCursor(cursor storage.Cursor, scope []byte) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(h.cursorMAC(payload, scope))
}
func (h *handler) decodeCursor(cursor string, scope []byte) (storage.Cursor, error) {
	var c storage.Cursor
	if len(cursor) > 512 {
		return c, fmt.Errorf("invalid cursor")
	}
	parts := strings.Split(cursor, ".")
	if len(parts) != 2 {
		return c, fmt.Errorf("invalid cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return c, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, err
	}
	if !hmac.Equal(signature, h.cursorMAC(payload, scope)) {
		return c, fmt.Errorf("invalid cursor")
	}
	if err = json.Unmarshal(payload, &c); err != nil {
		return c, err
	}
	if c.ID <= 0 {
		return c, fmt.Errorf("invalid cursor")
	}
	return c, nil
}
