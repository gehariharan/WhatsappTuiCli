package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"whatsappsync/internal/query"
)

func isV1(path string) bool { return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/") }

// serveV1 runs after the shared Host and Origin checks; legacy auth and error
// shapes never apply to /api/v1.
func (h *handler) serveV1(w http.ResponseWriter, r *http.Request) {
	id := query.NewRequestID()
	w.Header().Set("X-Request-Id", id)
	if r.URL.Path == "/api/v1/version" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			v1Fail(w, id, 405, query.CodeInvalidArgument, "method not allowed", "use GET /api/v1/version")
			return
		}
		writeJSON(w, 200, query.V1Response{RequestID: id, Data: query.Version()})
		return
	}
	if !h.v1Authorize(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		v1Fail(w, id, 401, query.CodeUnauthorized, "bearer token required", "send Authorization: Bearer <api.token>")
		return
	}
	v1Fail(w, id, 404, query.CodeNotFound, "unknown v1 endpoint", "see GET /api/v1/version for supported features")
}

// v1Authorize is the single v1 credential decision; scoped agent credentials replace it.
func (h *handler) v1Authorize(r *http.Request) bool {
	return h.token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.token)) == 1
}

func v1Fail(w http.ResponseWriter, id string, code int, errorCode, message, help string) {
	writeJSON(w, code, query.V1Error{RequestID: id, ErrorCode: errorCode, Error: message, Help: help})
}
