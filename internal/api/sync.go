package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"whatsappsync/internal/storage"
	"whatsappsync/internal/whatsapp"
)

func (h *handler) syncNow(w http.ResponseWriter, r *http.Request) {
	if h.syncer == nil {
		fail(w, 503, "sync owner unavailable")
		return
	}
	var in whatsapp.SyncInput
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		fail(w, 400, "invalid sync request")
		return
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "one request required")
		return
	}
	if in.WaitSeconds < 0 || in.WaitSeconds > 60 {
		fail(w, 400, "wait must be 0 to 60 seconds")
		return
	}
	result, err := h.syncer(r.Context(), in)
	if err != nil {
		writeJSON(w, 409, map[string]any{"result": result, "error": err.Error()})
		return
	}
	writeJSON(w, 200, result)
}
func (h *handler) enableAll(w http.ResponseWriter, r *http.Request) {
	account := h.account(w, r)
	if account == nil {
		return
	}
	chats, err := h.store.Groups(r.Context(), account.ID)
	if err != nil {
		fail(w, 500, "archive unavailable")
		return
	}
	ids := make([]string, 0, len(chats))
	for _, chat := range chats {
		if strings.HasPrefix(r.URL.Path, "/api/chats") || storage.ChatKind(chat.ID) == "group" {
			ids = append(ids, chat.ID)
		}
	}
	enabled := strings.HasSuffix(r.URL.Path, "/enable-all")
	if err = h.store.SetEnabled(r.Context(), account.ID, ids, enabled); err != nil {
		fail(w, 500, "cannot update preferences")
		return
	}
	writeJSON(w, 200, map[string]any{"changed": len(ids), "sync_enabled": enabled})
}
