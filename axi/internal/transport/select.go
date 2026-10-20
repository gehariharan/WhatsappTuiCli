package transport

import (
	"context"
	"time"

	"github.com/gehariharan/wasync/axi/internal/contract"
)

// SelectOptions configures transport selection. Either transport may be
// nil when it cannot be built (bad address, wasync not installed); the
// matching *Err field then explains why.
type SelectOptions struct {
	API      Transport
	APIErr   error
	Query    Transport
	QueryErr error
	// Accept rejects an incompatible version (e.g. compat.CheckAPI).
	Accept func(contract.VersionInfo) error
	// ProbeTimeout bounds the API probe; default 750ms.
	ProbeTimeout time.Duration
	// QueryTimeout bounds the offline version probe; default 10s.
	QueryTimeout time.Duration
}

// Selection is the outcome of Select.
type Selection struct {
	Transport Transport // nil when neither transport works
	// Version is the version reported by the chosen transport, or by the
	// last transport that answered but was rejected as incompatible.
	Version *contract.VersionInfo
	// APIErr is why the API was not used (nil when it was).
	APIErr error
	// QueryErr is why the query transport was not used (nil when it was or
	// when it was never tried).
	QueryErr error
	// Incompatible is set when a transport answered but Accept rejected it.
	Incompatible error
}

// Name returns the chosen transport name, or "none".
func (s Selection) Name() string {
	if s.Transport == nil {
		return "none"
	}
	return s.Transport.Name()
}

// Select probes GET /api/v1/version with a short timeout and uses the API
// when it answers with a compatible version; otherwise it falls back to
// `wasync query version`. The decision is deterministic (ADR-001 D-1).
func Select(ctx context.Context, o SelectOptions) Selection {
	if o.ProbeTimeout <= 0 {
		o.ProbeTimeout = 750 * time.Millisecond
	}
	if o.QueryTimeout <= 0 {
		o.QueryTimeout = 10 * time.Second
	}
	var sel Selection
	if o.API != nil {
		pctx, cancel := context.WithTimeout(ctx, o.ProbeTimeout)
		v, err := o.API.Version(pctx)
		cancel()
		if err == nil && o.Accept != nil {
			if aerr := o.Accept(v); aerr != nil {
				err = aerr
				sel.Incompatible = aerr
				sel.Version = &v
			}
		}
		if err == nil {
			sel.Transport, sel.Version = o.API, &v
			return sel
		}
		sel.APIErr = err
	} else {
		sel.APIErr = o.APIErr
	}
	if o.Query == nil {
		sel.QueryErr = o.QueryErr
		return sel
	}
	qctx, cancel := context.WithTimeout(ctx, o.QueryTimeout)
	defer cancel()
	v, err := o.Query.Version(qctx)
	if err == nil && o.Accept != nil {
		if aerr := o.Accept(v); aerr != nil {
			sel.QueryErr, sel.Incompatible = aerr, aerr
			sel.Version = &v
			return sel
		}
	}
	if err != nil {
		sel.QueryErr = err
		return sel
	}
	sel.Transport, sel.Version = o.Query, &v
	return sel
}
