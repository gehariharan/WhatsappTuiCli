package api

import (
	"encoding/json"
	"io"
	"net/http"
	"whatsappsync/internal/whatsapp"
)

func (h *handler) mediaControl(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Chat    string `json:"chat_id"`
		Message string `json:"message_id"`
		Mode    string `json:"mode"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		fail(w, 400, "invalid media request")
		return
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "one request required")
		return
	}
	if _, err := whatsapp.ParseChat(in.Chat); err != nil {
		fail(w, 400, err.Error())
		return
	}
	account := h.account(w, r)
	if account == nil {
		return
	}
	if r.URL.Path == "/api/media/policy" {
		if in.Message != "" {
			fail(w, 400, "policy does not accept message_id")
			return
		}
		if in.Mode != "" {
			if err := h.store.SetMediaPolicy(r.Context(), account.ID, in.Chat, in.Mode); err != nil {
				fail(w, 400, err.Error())
				return
			}
		}
		mode, err := h.store.MediaPolicy(r.Context(), account.ID, in.Chat)
		if err != nil {
			fail(w, 404, "chat not found")
			return
		}
		writeJSON(w, 200, map[string]any{"chat_id": in.Chat, "mode": mode, "persistent": true})
		return
	}
	if in.Message == "" || len(in.Message) > 1024 || in.Mode != "" {
		fail(w, 400, "provide only chat_id and message_id")
		return
	}
	if h.mediaDownload == nil {
		fail(w, 503, "live media owner unavailable; start the TUI or serve")
		return
	}
	result, err := h.mediaDownload(r.Context(), in.Chat, in.Message)
	if err != nil {
		writeJSON(w, 409, map[string]any{"result": result, "error": err.Error()})
		return
	}
	writeJSON(w, 202, result)
}
