package cli

import (
	"context"
	"errors"
	"strconv"

	"github.com/gehariharan/whatsapp-axi/internal/buildinfo"
	"github.com/gehariharan/whatsapp-axi/internal/config"
	"github.com/gehariharan/whatsapp-axi/internal/contract"
	"github.com/gehariharan/whatsapp-axi/internal/output"
	"github.com/gehariharan/whatsapp-axi/internal/transport"
)

// wasync states shown by the home and version views.
const (
	StateOnline       = "online"        // API reachable and compatible
	StateOffline      = "offline"       // no owner; offline reads via wasync query
	StateIncompatible = "incompatible"  // wasync answered but the versions do not match
	StateNotInstalled = "not_installed" // no API and no wasync binary
	StateUnavailable  = "unavailable"   // wasync found but neither transport answered
)

type detection struct {
	settings  config.Settings
	sel       transport.Selection
	bin       string
	binErr    error
	cred      *config.Credential
	credErr   error
	compatErr error
	state     string
}

// detect resolves settings, probes the transports and checks compatibility
// for cmd. It never fails: problems are reported as state.
func (a *App) detect(ctx context.Context, cmd string) detection {
	d := detection{settings: config.Load(a.Getenv)}
	d.cred, d.credErr = config.LoadCredential(a.Getenv, d.settings.ConfigDir)
	opts := transport.SelectOptions{Accept: a.Compat.CheckAPI, ProbeTimeout: a.ProbeTimeout, QueryTimeout: a.QueryTimeout}
	if api, err := transport.NewAPI(transport.APIConfig{Addr: d.settings.APIAddr, Credential: d.cred}); err != nil {
		opts.APIErr = err
	} else {
		opts.API = api
	}
	if bin, err := transport.ResolveBin(d.settings.Bin); err != nil {
		d.binErr, opts.QueryErr = err, err
	} else {
		d.bin = bin
		opts.Query = transport.NewQuery(transport.QueryConfig{Bin: bin, DataDir: d.settings.DataDir})
	}
	d.sel = transport.Select(ctx, opts)
	switch {
	case d.sel.Transport != nil && d.sel.Version != nil:
		d.compatErr = a.Compat.Check(cmd, *d.sel.Version)
	case d.sel.Incompatible != nil:
		d.compatErr = d.sel.Incompatible
	case errCode(d.sel.QueryErr) == contract.CodeWasyncTooOld:
		d.compatErr = d.sel.QueryErr
	}
	switch {
	case d.sel.Name() == transport.NameAPI:
		d.state = StateOnline
	case d.sel.Name() == transport.NameQuery:
		d.state = StateOffline
	case d.sel.Incompatible != nil:
		d.state = StateIncompatible
	case d.binErr != nil:
		d.state = StateNotInstalled
	default:
		d.state = StateUnavailable
	}
	return d
}

func (d detection) credentialState() string {
	switch {
	case d.credErr != nil:
		return "invalid"
	case d.cred == nil:
		return "missing"
	case d.cred.Source == "env":
		return "env " + config.EnvToken
	default:
		return "file"
	}
}

func errCode(err error) string {
	var ce *contract.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	if err == nil {
		return ""
	}
	return contract.CodeInternal
}

type compatJSON struct {
	OK     bool   `json:"ok"`
	Status string `json:"status"` // "ok", "unknown" or an error code such as wasync_too_old
	Reason string `json:"reason,omitempty"`
	Help   string `json:"help,omitempty"`
}

type wasyncJSON struct {
	State      string                `json:"state"`
	Transport  string                `json:"transport"`
	APIAddr    string                `json:"api_addr"`
	APIError   *output.ErrorJSON     `json:"api_error,omitempty"`
	Bin        string                `json:"bin,omitempty"`
	BinError   *output.ErrorJSON     `json:"bin_error,omitempty"`
	QueryError *output.ErrorJSON     `json:"query_error,omitempty"`
	Version    *contract.VersionInfo `json:"version"`
	Credential string                `json:"credential"`
}

func errJSON(err error) *output.ErrorJSON {
	if err == nil {
		return nil
	}
	e := output.ToErrorJSON(err)
	return &e
}

func (a *App) wasyncJSON(d detection) wasyncJSON {
	w := wasyncJSON{State: d.state, Transport: d.sel.Name(), APIAddr: d.settings.APIAddr, Version: d.sel.Version, Credential: d.credentialState()}
	if d.sel.Name() != transport.NameAPI {
		w.APIError = errJSON(d.sel.APIErr)
	}
	if d.bin != "" {
		w.Bin = a.collapseHome(d.bin)
	}
	w.BinError = errJSON(d.binErr)
	if d.sel.Transport == nil && d.binErr == nil {
		w.QueryError = errJSON(d.sel.QueryErr)
	}
	return w
}

func compatOf(d detection) compatJSON {
	if d.compatErr != nil {
		e := output.ToErrorJSON(d.compatErr)
		return compatJSON{Status: e.ErrorCode, Reason: e.Error, Help: e.Help}
	}
	if d.sel.Version == nil {
		return compatJSON{Status: "unknown", Reason: "wasync version not detected"}
	}
	return compatJSON{OK: true, Status: "ok"}
}

// writeWasync writes the shared `wasync:` and `compatibility:` blocks.
// detail adds the feature list and the compatibility fix, for views that
// print no help hints.
func (a *App) writeWasync(o *output.Writer, d detection, detail bool) {
	w := a.wasyncJSON(d)
	o.Object(0, "wasync")
	o.Field(1, "state", w.State)
	o.Field(1, "transport", w.Transport)
	if w.APIError != nil {
		o.Field(1, "api", w.APIAddr+" ("+w.APIError.ErrorCode+")")
	} else {
		o.Field(1, "api", w.APIAddr)
	}
	if w.Bin != "" {
		o.Field(1, "bin", w.Bin)
	}
	if w.QueryError != nil {
		o.Field(1, "query", w.QueryError.ErrorCode+": "+w.QueryError.Error)
	}
	if v := d.sel.Version; v != nil {
		o.Field(1, "version", v.WasyncVersion)
		o.Field(1, "api_version", v.API())
		o.Field(1, "schema", strconv.Itoa(v.SchemaVersion))
		if v.ArchiveSchemaVersion != nil {
			o.Field(1, "archive_schema", strconv.Itoa(*v.ArchiveSchemaVersion))
		}
		if detail {
			o.List(1, "features", v.Features)
		}
	}
	o.Field(1, "credential", w.Credential)
	if d.state == StateOffline {
		o.Field(1, "note", "offline reads come straight from the local archive and are not scoped by an agent grant")
	}
	c := compatOf(d)
	o.Object(0, "compatibility")
	o.Field(1, "status", c.Status)
	if c.Reason != "" {
		o.Field(1, "reason", c.Reason)
	}
	if detail && c.Help != "" {
		o.Field(1, "fix", c.Help)
	}
}

func (a *App) hints(d detection) []string {
	var h []string
	switch d.state {
	case StateNotInstalled:
		h = append(h, "Install wasync: brew install --cask gehariharan/tap/wasync",
			"If wasync is installed elsewhere, set "+config.EnvBin+"=<path to wasync>")
	case StateUnavailable:
		var ce *contract.Error
		if errors.As(d.sel.QueryErr, &ce) && ce.Help != "" {
			h = append(h, ce.Help)
		}
		h = append(h, "Run `wasync query status` to see why wasync cannot read its archive",
			"Set "+config.EnvDataDir+"=<dir> if wasync uses a non-default data directory")
	case StateIncompatible:
		if c := compatOf(d); c.Help != "" {
			h = append(h, c.Help)
		}
	case StateOffline:
		h = append(h, "Ask the user to start wasync (the TUI or `wasync serve`) for live, scoped reads; whatsapp-axi never starts it")
	}
	if d.credErr != nil {
		var ce *contract.Error
		if errors.As(d.credErr, &ce) && ce.Help != "" {
			h = append(h, "Credential problem: "+ce.Message+"; "+ce.Help)
		}
	} else if d.cred == nil && (d.state == StateOnline || d.state == StateOffline) {
		h = append(h, "Ask the user to run `wasync agents grant <name> --scope read.chats,read.messages`, then set "+config.EnvToken+" or save it to "+a.collapseHome(d.settings.ConfigDir)+"/"+config.CredentialFile+" (mode 600)")
	}
	if d.compatErr != nil && d.state != StateIncompatible {
		if c := compatOf(d); c.Help != "" {
			h = append(h, c.Help)
		}
	}
	h = append(h, "Run `whatsapp-axi version` for wasync features", "Run `whatsapp-axi help` for commands")
	return dedupe(h)
}

func dedupe(lines []string) []string {
	seen := make(map[string]bool, len(lines))
	out := lines[:0]
	for _, l := range lines {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

func (a *App) runHome(ctx context.Context, inv invocation) error {
	d := a.detect(ctx, "home")
	hints := a.hints(d)
	if inv.json {
		return output.JSON(a.Out, map[string]any{
			"bin": a.binPath(), "description": Description, "whatsapp_axi_version": buildinfo.Version,
			"wasync": a.wasyncJSON(d), "compatibility": compatOf(d), "help": hints,
		})
	}
	o := output.New(a.Out)
	o.Field(0, "bin", a.binPath())
	o.Field(0, "description", Description)
	a.writeWasync(o, d, false)
	o.Help(hints...)
	return o.Err()
}

func (a *App) runVersion(ctx context.Context, inv invocation) error {
	d := a.detect(ctx, "version")
	if inv.json {
		return output.JSON(a.Out, map[string]any{
			"whatsapp_axi_version": buildinfo.Version, "api_version_supported": contract.APIVersion,
			"min_api": a.Compat.MinAPI, "wasync": a.wasyncJSON(d), "compatibility": compatOf(d),
		})
	}
	o := output.New(a.Out)
	o.Field(0, "whatsapp-axi", buildinfo.Version)
	o.Field(0, "min_api", a.Compat.MinAPI)
	a.writeWasync(o, d, true)
	return o.Err()
}
