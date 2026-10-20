// Package transport reads wasync through exactly the two transports allowed
// by ADR-001: the loopback /api/v1 HTTP API and the `wasync query --v1 --json`
// subprocess. Both decode the same v1 envelope.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/gehariharan/wasync/axi/internal/contract"
)

// Transport is one way of reading wasync.
type Transport interface {
	// Version performs the version handshake.
	Version(ctx context.Context) (contract.VersionInfo, error)
	// Get reads path (relative to /api/v1, e.g. "/chats") with query
	// parameters. out receives the envelope's data, or the whole success
	// envelope when it is a *contract.Response. Error envelopes are
	// returned as *contract.Error.
	Get(ctx context.Context, path string, query url.Values, out any) error
	// Name is "api" or "query".
	Name() string
}

// Transport names.
const (
	NameAPI   = "api"
	NameQuery = "query"
)

// maxBody bounds any envelope read from either transport.
const maxBody = 16 << 20

// validatePath accepts only clean absolute paths relative to /api/v1.
func validatePath(path string) error {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\\") || strings.Contains(path, "..") || strings.Contains(path, "//") {
		return contract.Errorf(contract.CodeInvalidArgument, "", "invalid API path %q", path)
	}
	return nil
}

// decodeEnvelope parses a v1 envelope and stores its result in out.
func decodeEnvelope(body []byte, transportName string, out any) error {
	var env contract.Envelope
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&env); err != nil {
		return &contract.Error{Code: contract.CodeInvalidResponse, Message: "wasync returned a response that is not a v1 envelope", Help: "check that wasync is up to date: brew tap --custom-remote gehariharan/tap https://github.com/gehariharan/wasync.git && brew upgrade --cask gehariharan/tap/wasync", Transport: transportName}
	}
	if env.IsError() {
		e := env.Err()
		e.Transport = transportName
		return e
	}
	if out == nil {
		return nil
	}
	if r, ok := out.(*contract.Response); ok {
		*r = contract.Response{RequestID: env.RequestID, Data: env.Data, Page: env.Page}
		return nil
	}
	if len(env.Data) == 0 {
		return &contract.Error{RequestID: env.RequestID, Code: contract.CodeInvalidResponse, Message: "wasync response has no data", Transport: transportName}
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return &contract.Error{RequestID: env.RequestID, Code: contract.CodeInvalidResponse, Message: fmt.Sprintf("wasync response data has an unexpected shape: %v", err), Transport: transportName}
	}
	return nil
}
