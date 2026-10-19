package api

import (
	"context"
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
	h.v1Once.Do(h.buildV1)
	h.v1Mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), v1IDKey{}, id)))
}

type v1IDKey struct{}

// v1Route is one versioned endpoint. Scope names the agent permission the
// route needs; Chat names the path value holding the chat JID, if any.
type v1Route struct {
	Pattern, Scope, Chat string
	Handle               func(h *handler, w http.ResponseWriter, r *http.Request, id string)
}

// v1Routes is filled by feature files in init, keeping endpoints independent.
var v1Routes []v1Route

func registerV1(route v1Route) { v1Routes = append(v1Routes, route) }

func (h *handler) buildV1() {
	h.v1Mux = http.NewServeMux()
	for _, route := range v1Routes {
		h.v1Mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, r *http.Request) {
			id, _ := r.Context().Value(v1IDKey{}).(string)
			chat := ""
			if route.Chat != "" {
				chat = r.PathValue(route.Chat)
			}
			if !h.v1Authorize(w, r, id, route.Scope, chat) {
				return
			}
			route.Handle(h, w, r, id)
		})
	}
	h.v1Mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		id, _ := r.Context().Value(v1IDKey{}).(string)
		if !h.v1Authorize(w, r, id, "", "") {
			return
		}
		v1Fail(w, id, 404, query.CodeNotFound, "unknown v1 endpoint", "see GET /api/v1/version for supported features")
	})
}

// v1Authorize is the single v1 credential decision and writes the failure
// itself. Scoped agent credentials replace the bearer check (W-09).
func (h *handler) v1Authorize(w http.ResponseWriter, r *http.Request, id, scope, chat string) bool {
	if h.token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.token)) == 1 {
		return true
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	v1Fail(w, id, 401, query.CodeUnauthorized, "bearer token required", "send Authorization: Bearer <api.token>")
	return false
}

func v1Fail(w http.ResponseWriter, id string, code int, errorCode, message, help string) {
	writeJSON(w, code, query.V1Error{RequestID: id, ErrorCode: errorCode, Error: message, Help: help})
}
