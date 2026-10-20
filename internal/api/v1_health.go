package api

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gehariharan/wasync/internal/agentauth"
	"github.com/gehariharan/wasync/internal/query"
	"github.com/gehariharan/wasync/internal/storage"
)

func init() {
	registerV1(v1Route{Pattern: "GET /api/v1/health/deep", Scope: agentauth.ScopeReadChats, Handle: (*handler).v1HealthDeep})
	// Scope "" means v1Authorize only authenticates: any valid agent may run doctor.
	registerV1(v1Route{Pattern: "GET /api/v1/diagnostics", Scope: "", Handle: (*handler).v1Diagnostics})
	query.RegisterFeature("health.deep")
	query.RegisterFeature("diagnostics")
}

const v1HealthTimeout = 5 * time.Second

// deepHealth answers from the owner's in-memory facts and the archive; it
// never contacts WhatsApp. Without a live client (serve --offline) the
// state is offline.
func (h *handler) deepHealth(ctx context.Context) query.HealthDeep {
	d := query.HealthDeep{State: query.StateOffline, OwnerRunning: true}
	if h.status != nil {
		s := h.status()
		d.State, d.Connected = query.HealthState(s.Connection, s.KeepAliveFailing)
		d.KeepAliveFailing = s.KeepAliveFailing
		d.LastEventAt = query.TimePointer(s.LastEventAt)
		d.LastConnectedAt = query.TimePointer(s.LastConnectedAt)
		d.LastKeepAliveOKAt = query.TimePointer(s.LastKeepAliveOK)
		d.CaptureError = query.CaptureErrorSummary(d.State, s.Error)
	}
	if counts, err := h.store.HealthCounts(ctx); err == nil {
		d.ArchiveReady = true
		d.SchemaVersion = counts.SchemaVersion
		d.OpenGaps = counts.OpenGaps
		d.HistoryJobsActive = counts.HistoryJobsActive
	}
	return d
}

func (h *handler) v1HealthDeep(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), v1HealthTimeout)
	defer cancel()
	writeJSON(w, 200, query.V1Response{RequestID: id, Data: h.deepHealth(ctx)})
}

func (h *handler) v1Diagnostics(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), v1HealthTimeout)
	defer cancel()
	dir := h.agents.Dir()
	var paired *bool
	if h.status != nil {
		p := h.status().Paired
		paired = &p
	} else if _, p, err := storage.ProbeSession(ctx, filepath.Join(dir, "session.db")); err == nil {
		// serve --offline holds the device lock but no client; read the
		// session store read-only instead of connecting.
		paired = &p
	}
	version := query.Version().APIVersion
	data := query.Diagnose(ctx, query.DiagnosticsInput{
		Transport: "api", DataDir: dir, OwnerRunning: true, APIVersion: &version,
		Health: h.deepHealth(ctx).State, Paired: paired, Agent: v1Client(r),
	})
	writeJSON(w, 200, query.V1Response{RequestID: id, Data: data})
}
