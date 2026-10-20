package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/gehariharan/wasync/internal/config"
	"github.com/gehariharan/wasync/internal/processlock"
	"github.com/gehariharan/wasync/internal/query"
	"github.com/gehariharan/wasync/internal/storage"
)

const queryDoctorUsage = `Usage: wasync [--data-dir PATH] query doctor [--json]

Report installation, data directory, archive, pairing, owner and agent facts
with pass/warn/fail checks and fix commands, as a v1 envelope. Never connects
to WhatsApp or the network and never creates or changes files. An owner is
detected by probing device.lock without holding it; its live connection state
is available only from GET /api/v1/health/deep.
`

func (a *App) runQueryDoctor(ctx context.Context, cfg config.Config, args []string) (result error) {
	defer func() { result = a.v1Failure(true, result) }()
	fs := a.flags("query doctor")
	jsonOutput := fs.Bool("json", true, "JSON output (default)")
	fs.Bool("v1", true, "v1 envelope (always)")
	if err := a.parseFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return queryErr(query.CodeInvalidArgument, err)
	}
	if fs.NArg() != 0 || !*jsonOutput {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("usage: wasync query doctor [--json]"))
	}
	return a.queryOutput(true, offlineDiagnostics(ctx, cfg.DataDir), nil)
}

// offlineDiagnostics never holds device.lock and never opens WhatsApp.
func offlineDiagnostics(ctx context.Context, dir string) query.Diagnostics {
	in := query.DiagnosticsInput{Transport: "query", DataDir: dir, Health: query.StateOffline}
	held, err := processlock.Held(filepath.Join(dir, "device.lock"))
	in.OwnerRunning = held
	if held || err != nil {
		in.Health = query.StateUnknown
	}
	if _, paired, err := storage.ProbeSession(ctx, filepath.Join(dir, "session.db")); err == nil {
		in.Paired = &paired
	}
	return query.Diagnose(ctx, in)
}
