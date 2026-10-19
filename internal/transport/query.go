package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/gehariharan/whatsapp-axi/internal/config"
	"github.com/gehariharan/whatsapp-axi/internal/contract"
)

// InstallHelp tells a human how to install wasync.
const InstallHelp = "install wasync (brew install --cask gehariharan/tap/wasync) or set " + config.EnvBin + " to its path"

// QueryRoute maps one read-only /api/v1 path to a `wasync query` command.
type QueryRoute struct {
	// Command is the argument list after `query`, e.g. ["version"].
	Command []string
	// Params maps accepted API query parameters to wasync query flags.
	// Parameters not listed are rejected rather than silently dropped.
	Params map[string]string
	// PathFlag, for a route whose key ends in one "{name}" segment, is the
	// wasync query flag that receives that segment.
	PathFlag string
}

// lookupQueryRoute finds the route for path: an exact key, or a key ending
// in "/{name}" whose final segment is non-empty. It returns the route and
// the flags carrying path segments.
func lookupQueryRoute(path string) (QueryRoute, []string, bool) {
	if route, ok := queryRoutes[path]; ok {
		return route, nil, true
	}
	for _, key := range QueryRoutes() {
		route := queryRoutes[key]
		i := strings.Index(key, "{")
		if i < 0 || route.PathFlag == "" || !strings.HasPrefix(path, key[:i]) {
			continue
		}
		if seg := path[i:]; seg != "" && !strings.Contains(seg, "/") {
			return route, []string{"--" + route.PathFlag, seg}, true
		}
	}
	return QueryRoute{}, nil, false
}

// queryRoutes is the API path to offline command table. Only reads belong
// here; every future v1 read endpoint adds its `wasync query` equivalent.
var queryRoutes = map[string]QueryRoute{
	"/version": {Command: []string{"version"}},
}

// QueryRoutes returns the mapped API paths, sorted.
func QueryRoutes() []string {
	out := make([]string, 0, len(queryRoutes))
	for p := range queryRoutes {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// QueryArgs builds the full wasync argument list for path and query. Global
// flags (--data-dir) precede the `query` subcommand, as wasync expects.
func QueryArgs(dataDir, path string, query url.Values) ([]string, error) {
	route, pathArgs, ok := lookupQueryRoute(path)
	if !ok {
		return nil, &contract.Error{Code: contract.CodeOwnerOffline, Message: "this read needs the running wasync owner; it has no offline equivalent", Help: "start wasync (TUI or `wasync serve`) and retry", Transport: NameQuery}
	}
	var args []string
	if dataDir != "" {
		args = append(args, "--data-dir", dataDir)
	}
	args = append(args, "query")
	args = append(args, route.Command...)
	args = append(args, pathArgs...)
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		flag, ok := route.Params[k]
		if !ok {
			return nil, contract.Errorf(contract.CodeInvalidArgument, "", "parameter %q is not supported offline for %s", k, path)
		}
		for _, v := range query[k] {
			args = append(args, "--"+flag, v)
		}
	}
	return append(args, "--v1", "--json"), nil
}

// ResolveBin finds wasync: bin when set, otherwise `wasync` on PATH.
func ResolveBin(bin string) (string, error) {
	if bin != "" {
		info, err := os.Stat(bin)
		if err != nil || info.IsDir() {
			return "", contract.Errorf(contract.CodeWasyncNotFound, InstallHelp, "%s=%s does not point at a wasync binary", config.EnvBin, bin)
		}
		return bin, nil
	}
	p, err := exec.LookPath("wasync")
	if err != nil {
		return "", contract.Errorf(contract.CodeWasyncNotFound, InstallHelp, "wasync is not on PATH")
	}
	return p, nil
}

// QueryConfig configures the subprocess transport.
type QueryConfig struct {
	Bin     string // resolved wasync path
	DataDir string // passed as --data-dir when set
	Timeout time.Duration
}

// Query reads the archive offline through `wasync query --v1 --json`.
type Query struct{ cfg QueryConfig }

// NewQuery builds the subprocess transport.
func NewQuery(cfg QueryConfig) *Query {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Query{cfg: cfg}
}

// Name implements Transport.
func (q *Query) Name() string { return NameQuery }

// Bin returns the wasync path this transport runs.
func (q *Query) Bin() string { return q.cfg.Bin }

// Version implements Transport.
func (q *Query) Version(ctx context.Context) (contract.VersionInfo, error) {
	var v contract.VersionInfo
	err := q.Get(ctx, "/version", nil, &v)
	var ce *contract.Error
	if errors.As(err, &ce) && ce.Code == contract.CodeInvalidResponse {
		ce.Code = contract.CodeWasyncTooOld
		ce.Message = "this wasync does not support `wasync query version --v1`"
		ce.Help = "upgrade wasync: brew upgrade --cask gehariharan/tap/wasync"
	}
	return v, err
}

// Get implements Transport.
func (q *Query) Get(ctx context.Context, path string, query url.Values, out any) error {
	if err := validatePath(path); err != nil {
		return err
	}
	args, err := QueryArgs(q.cfg.DataDir, path, query)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, q.cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, q.cfg.Bin, args...)
	cmd.Env = childEnv(os.Environ())
	cmd.Stdin = nil
	stdout := &limitedBuffer{max: maxBody}
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard // raw dependency output never reaches the agent
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return &contract.Error{Code: contract.CodeTransport, Message: "wasync query timed out", Help: "retry; if it persists run `wasync query status`", Transport: NameQuery}
	}
	if stdout.overflow {
		return &contract.Error{Code: contract.CodeInvalidResponse, Message: "wasync query output is too large", Transport: NameQuery}
	}
	if runErr != nil {
		var ee *exec.ExitError
		if !errors.As(runErr, &ee) {
			return &contract.Error{Code: contract.CodeWasyncNotFound, Message: "cannot run wasync", Help: InstallHelp, Transport: NameQuery}
		}
		if len(bytes.TrimSpace(stdout.Bytes())) == 0 {
			return &contract.Error{Code: contract.CodeInvalidResponse, Message: "wasync query failed without a v1 envelope", Help: "upgrade wasync: brew upgrade --cask gehariharan/tap/wasync", Transport: NameQuery}
		}
	}
	return decodeEnvelope(stdout.Bytes(), NameQuery, out)
}

// childEnv drops whatsapp-axi's own credential from the subprocess env:
// offline reads are unscoped and never need it.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, config.EnvToken+"=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

type limitedBuffer struct {
	bytes.Buffer
	max      int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		b.overflow = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
