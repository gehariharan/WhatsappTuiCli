package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"time"
)

// SignSend authenticates a body without transmitting the send credential.
func SignCommand(token, timestamp, nonce, method, path string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(timestamp + "\n" + nonce + "\n" + method + "\n" + path + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
func (h *handler) authenticateSend(w http.ResponseWriter, r *http.Request, token string) bool {
	stamp := r.Header.Get("X-Wasync-Time")
	nonce := r.Header.Get("X-Wasync-Nonce")
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	nonceBytes, nonceErr := hex.DecodeString(nonce)
	now := time.Now()
	if err != nil || nonceErr != nil || len(nonceBytes) != 16 || seconds < now.Add(-time.Minute).Unix() || seconds > now.Add(time.Minute).Unix() {
		fail(w, 401, "invalid send authentication")
		return false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128<<10))
	if err != nil {
		fail(w, 400, "invalid send request")
		return false
	}
	expected := SignCommand(token, stamp, nonce, r.Method, r.URL.Path, body)
	if !hmac.Equal([]byte(expected), []byte(r.Header.Get("X-Wasync-Signature"))) {
		fail(w, 401, "invalid send authentication")
		return false
	}
	h.sendAuthMu.Lock()
	defer h.sendAuthMu.Unlock()
	if h.sendNonces == nil {
		h.sendNonces = make(map[string]time.Time)
	}
	for id, expiry := range h.sendNonces {
		if now.After(expiry) {
			delete(h.sendNonces, id)
		}
	}
	if _, exists := h.sendNonces[nonce]; exists {
		fail(w, 409, "send request already used; check original outcome")
		return false
	}
	if len(h.sendNonces) >= 4096 {
		fail(w, 429, "too many send requests")
		return false
	}
	h.sendNonces[nonce] = now.Add(2 * time.Minute)
	r.Body = io.NopCloser(bytes.NewReader(body))
	return true
}
