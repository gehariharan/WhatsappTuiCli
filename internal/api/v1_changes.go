package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"whatsappsync/internal/agentauth"
	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
)

// Change journal endpoints (ADR-001 D-4). Reading changes and checkpoints
// needs observe; committing a checkpoint is a mutation under the same scope
// and is always audited.
func init() {
	registerV1(v1Route{Pattern: "GET /api/v1/changes", Scope: agentauth.ScopeObserve, Handle: (*handler).v1Changes})
	registerV1(v1Route{Pattern: "/api/v1/changes", Scope: agentauth.ScopeObserve, Handle: v1MethodNotAllowed("GET, HEAD", "use GET /api/v1/changes?after=CURSOR&limit=N")})
	registerV1(v1Route{Pattern: "GET /api/v1/checkpoints/{consumer}", Scope: agentauth.ScopeObserve, Handle: (*handler).v1GetCheckpoint})
	registerV1(v1Route{Pattern: "PUT /api/v1/checkpoints/{consumer}", Scope: agentauth.ScopeObserve, Mutation: true, Handle: (*handler).v1PutCheckpoint})
	registerV1(v1Route{Pattern: "/api/v1/checkpoints/{consumer}", Scope: agentauth.ScopeObserve, Handle: v1MethodNotAllowed("GET, HEAD, PUT", `use GET or PUT /api/v1/checkpoints/{consumer} with {"cursor":"c1_..."}`)})
	query.RegisterFeature("changes")
	query.RegisterFeature("checkpoints")
}

func v1MethodNotAllowed(allow, help string) func(*handler, http.ResponseWriter, *http.Request, string) {
	return func(_ *handler, w http.ResponseWriter, _ *http.Request, id string) {
		w.Header().Set("Allow", allow)
		v1Fail(w, id, 405, query.CodeInvalidArgument, "method not allowed", help)
	}
}

// v1Account returns the paired account or writes the failure.
func (h *handler) v1Account(w http.ResponseWriter, r *http.Request, id string) *storage.Account {
	a, err := h.store.Account(r.Context())
	if err != nil {
		v1Fail(w, id, 503, query.CodeArchiveUnavailable, "archive unavailable", "check the wasync owner log")
		return nil
	}
	if a == nil {
		v1Fail(w, id, 409, query.CodeNoAccount, "no paired account in the archive", "pair WhatsApp in the TUI or with wasync login first")
		return nil
	}
	return a
}

// v1FailErr writes a coded query error, or internal for anything else.
func v1FailErr(w http.ResponseWriter, id string, err error) {
	var c *query.CodedError
	if !errors.As(err, &c) {
		v1Fail(w, id, 500, query.CodeInternal, "archive unavailable", "check the wasync owner log")
		return
	}
	status := 400
	switch c.Code {
	case query.CodeCursorExpired:
		status = 410
	case query.CodeNotFound:
		status = 404
	}
	v1Fail(w, id, status, c.Code, c.Error(), c.Help)
}

func (h *handler) v1Changes(w http.ResponseWriter, r *http.Request, id string) {
	limit := query.DefaultChangeLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > query.MaxChangeLimit {
			v1Fail(w, id, 400, query.CodeInvalidArgument, "limit must be between 1 and 1000", "use limit=1..1000 (default 100)")
			return
		}
		limit = n
	}
	a := h.v1Account(w, r, id)
	if a == nil {
		return
	}
	c := v1Client(r)
	data, page, err := query.ReadChanges(r.Context(), h.store, a, r.URL.Query().Get("after"), limit, c.AllowsChat)
	if err != nil {
		v1FailErr(w, id, err)
		return
	}
	writeJSON(w, 200, query.V1Response{RequestID: id, Data: data, Page: page})
}

func (h *handler) v1GetCheckpoint(w http.ResponseWriter, r *http.Request, id string) {
	consumer := r.PathValue("consumer")
	if err := query.ValidConsumer(consumer); err != nil {
		v1FailErr(w, id, err)
		return
	}
	a := h.v1Account(w, r, id)
	if a == nil {
		return
	}
	cp, err := query.ReadCheckpoint(r.Context(), h.store, a, consumer)
	if err != nil {
		v1FailErr(w, id, err)
		return
	}
	writeJSON(w, 200, query.V1Response{RequestID: id, Data: cp})
}

func (h *handler) v1PutCheckpoint(w http.ResponseWriter, r *http.Request, id string) {
	consumer := r.PathValue("consumer")
	if err := query.ValidConsumer(consumer); err != nil {
		v1FailErr(w, id, err)
		return
	}
	const help = `send {"cursor":"c1_..."} with a cursor from GET /api/v1/changes`
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		v1Fail(w, id, 415, query.CodeInvalidArgument, "request body must be application/json", help)
		return
	}
	var body struct {
		Cursor string `json:"cursor"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Cursor == "" || decoder.Decode(&struct{}{}) != io.EOF {
		v1Fail(w, id, 400, query.CodeInvalidArgument, "invalid checkpoint body", help)
		return
	}
	a := h.v1Account(w, r, id)
	if a == nil {
		return
	}
	info, err := h.store.Journal(r.Context())
	if err != nil {
		v1FailErr(w, id, err)
		return
	}
	sequence, err := query.DecodeChangeCursor(body.Cursor, a.JID, info.Epoch)
	if err != nil {
		v1FailErr(w, id, err)
		return
	}
	cp, err := h.store.SetCheckpoint(r.Context(), a.ID, consumer, info.Epoch, sequence)
	switch {
	case errors.Is(err, storage.ErrCursorExpired):
		v1Fail(w, id, 410, query.CodeCursorExpired, err.Error(), query.HelpResnapshot)
		return
	case errors.Is(err, storage.ErrCheckpointBackwards):
		v1Fail(w, id, 409, query.CodeInvalidArgument, "checkpoint cannot move backwards", "Checkpoints only move forward; read the current one with GET /api/v1/checkpoints/"+consumer+" and commit a later cursor.")
		return
	case err != nil:
		v1FailErr(w, id, err)
		return
	}
	writeJSON(w, 200, query.V1Response{RequestID: id, Data: query.FormatCheckpoint(a.JID, cp)})
}
