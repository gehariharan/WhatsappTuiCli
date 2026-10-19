package agentauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"whatsappsync/internal/localfs"
	"whatsappsync/internal/processlock"
)

const (
	// FileName is the private client registry in the data directory.
	FileName = "agents.json"
	// LockFileName serializes writers (grant/revoke) across processes.
	LockFileName         = "agents.lock"
	FileVersion          = 1
	DefaultRatePerMinute = 60
	MaxRatePerMinute     = 6000
	maxFileSize          = 4 << 20
	lockWait             = 5 * time.Second
)

// Client is a registered agent client. It never carries the secret or its hash.
type Client struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Scopes        []string   `json:"scopes"`
	Chats         []string   `json:"chats"`
	RatePerMinute int        `json:"rate_per_minute"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
}

// HasScope reports whether the client was granted scope.
func (c *Client) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// AllowsChat reports whether chat is visible to this client: true for every
// chat when the client has no allowlist. List handlers use it to filter rows.
func (c *Client) AllowsChat(chat string) bool {
	if len(c.Chats) == 0 {
		return true
	}
	for _, allowed := range c.Chats {
		if allowed == chat {
			return true
		}
	}
	return false
}

// Status returns active, revoked or expired at now.
func (c *Client) Status(now time.Time) string {
	switch {
	case c.RevokedAt != nil:
		return "revoked"
	case c.ExpiresAt != nil && !now.Before(*c.ExpiresAt):
		return "expired"
	}
	return "active"
}

type record struct {
	Client
	SecretSHA256 string `json:"secret_sha256"`
}

type registry struct {
	Version int      `json:"version"`
	Clients []record `json:"clients"`
}

// GrantRequest describes a new client. Empty Scopes means DefaultScopes;
// zero RatePerMinute means DefaultRatePerMinute; zero Expires means no expiry.
type GrantRequest struct {
	Name          string
	Scopes        []string
	Chats         []string
	RatePerMinute int
	Expires       time.Duration
}

func (r *GrantRequest) normalize() error {
	if err := ValidName(r.Name); err != nil {
		return err
	}
	scopes, err := ParseScopes(r.Scopes...)
	if err != nil {
		return err
	}
	r.Scopes = scopes
	if r.Chats, err = normalizeChats(r.Chats); err != nil {
		return err
	}
	for _, s := range r.Scopes {
		if RequiresAllowlist(s) && len(r.Chats) == 0 {
			return fmt.Errorf("scope %s requires a chat allowlist; add --chat CHAT_JID for each permitted chat", s)
		}
	}
	if r.RatePerMinute == 0 {
		r.RatePerMinute = DefaultRatePerMinute
	}
	if r.RatePerMinute < 1 || r.RatePerMinute > MaxRatePerMinute {
		return fmt.Errorf("rate must be between 1 and %d requests per minute", MaxRatePerMinute)
	}
	if r.Expires < 0 {
		return fmt.Errorf("expiry must be positive")
	}
	return nil
}

// ValidateGrant checks a grant request without touching disk, so the CLI can
// reject bad input before asking for confirmation.
func ValidateGrant(r GrantRequest) error { return r.normalize() }

// List returns all clients (including revoked and expired), without secrets.
// A missing registry is an empty list.
func List(dir string) ([]Client, error) {
	reg, _, err := load(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Client, 0, len(reg.Clients))
	for _, r := range reg.Clients {
		out = append(out, r.Client)
	}
	return out, nil
}

// ActiveByName returns the non-revoked client with this name, if any.
func ActiveByName(dir, name string) (*Client, error) {
	clients, err := List(dir)
	if err != nil {
		return nil, err
	}
	for i := range clients {
		if clients[i].Name == name && clients[i].RevokedAt == nil {
			return &clients[i], nil
		}
	}
	return nil, nil
}

// Grant creates a client and returns the credential, which is never stored
// and cannot be recovered. Names must be unique among non-revoked clients.
func Grant(dir string, req GrantRequest, now time.Time) (credential string, client Client, err error) {
	if err = req.normalize(); err != nil {
		return "", Client{}, err
	}
	secret, err := newSecret()
	if err != nil {
		return "", Client{}, err
	}
	err = update(dir, func(reg *registry) error {
		ids := map[string]bool{}
		for _, r := range reg.Clients {
			ids[r.ID] = true
			if r.Name == req.Name && r.RevokedAt == nil {
				return fmt.Errorf("client %s already exists; revoke it first with wasync agents revoke %s", req.Name, req.Name)
			}
		}
		id := ""
		for attempt := 0; attempt < 16 && (id == "" || ids[id]); attempt++ {
			if id, err = newClientID(); err != nil {
				return err
			}
		}
		if ids[id] {
			return fmt.Errorf("could not allocate a client ID")
		}
		created := now.UTC().Truncate(time.Second)
		client = Client{ID: id, Name: req.Name, Scopes: req.Scopes, Chats: req.Chats, RatePerMinute: req.RatePerMinute, CreatedAt: created}
		if req.Expires > 0 {
			expires := created.Add(req.Expires)
			client.ExpiresAt = &expires
		}
		reg.Clients = append(reg.Clients, record{Client: client, SecretSHA256: hashSecret(secret)})
		return nil
	})
	if err != nil {
		return "", Client{}, err
	}
	return FormatCredential(client.ID, secret), client, nil
}

// Revoke marks the active client with this name revoked. The record is kept
// so audit entries remain attributable.
func Revoke(dir, name string, now time.Time) (client Client, err error) {
	if err = ValidName(name); err != nil {
		return Client{}, err
	}
	err = update(dir, func(reg *registry) error {
		for i := range reg.Clients {
			if reg.Clients[i].Name == name && reg.Clients[i].RevokedAt == nil {
				revoked := now.UTC().Truncate(time.Second)
				reg.Clients[i].RevokedAt = &revoked
				client = reg.Clients[i].Client
				return nil
			}
		}
		return fmt.Errorf("no active client named %s", name)
	})
	return client, err
}

// load reads and validates the registry. It refuses files that are not
// private, like api.token, and fails closed on any malformed entry.
func load(dir string) (*registry, os.FileInfo, error) {
	path := filepath.Join(dir, FileName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return &registry{Version: FileVersion}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if err = localfs.CheckPrivateFile(path); err != nil {
		return nil, nil, fmt.Errorf("%s must be a regular private file (0600, owner only): %w", FileName, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, nil, fmt.Errorf("%s changed while opening; retry", FileName)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > maxFileSize {
		return nil, nil, fmt.Errorf("%s is too large", FileName)
	}
	var reg registry
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&reg); err != nil {
		return nil, nil, fmt.Errorf("invalid %s: %w", FileName, err)
	}
	if reg.Version != FileVersion {
		return nil, nil, fmt.Errorf("unsupported %s version %d; upgrade wasync", FileName, reg.Version)
	}
	ids := map[string]bool{}
	active := map[string]bool{}
	for _, r := range reg.Clients {
		if err = validRecord(r); err != nil {
			return nil, nil, fmt.Errorf("invalid %s: %w", FileName, err)
		}
		if ids[r.ID] || (r.RevokedAt == nil && active[r.Name]) {
			return nil, nil, fmt.Errorf("invalid %s: duplicate client %s", FileName, r.Name)
		}
		ids[r.ID] = true
		if r.RevokedAt == nil {
			active[r.Name] = true
		}
	}
	return &reg, opened, nil
}

func validRecord(r record) error {
	if !validClientID(r.ID) || ValidName(r.Name) != nil || len(r.SecretSHA256) != 64 || r.RatePerMinute < 1 || r.RatePerMinute > MaxRatePerMinute || r.CreatedAt.IsZero() {
		return fmt.Errorf("malformed client entry")
	}
	for _, c := range r.SecretSHA256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return fmt.Errorf("malformed client entry")
		}
	}
	needsChats := false
	for _, s := range r.Scopes {
		if !ValidScope(s) {
			return fmt.Errorf("client %s has unknown scope %q", r.Name, s)
		}
		needsChats = needsChats || RequiresAllowlist(s)
	}
	for _, c := range r.Chats {
		if err := ValidChatJID(c); err != nil {
			return err
		}
	}
	if needsChats && len(r.Chats) == 0 {
		return fmt.Errorf("client %s has a write scope without a chat allowlist", r.Name)
	}
	return nil
}

// update applies fn under the cross-process writer lock and replaces the
// registry atomically (private temp file, fsync, rename, directory fsync).
// Readers never lock: they always see either the old or the new file.
func update(dir string, fn func(*registry) error) (result error) {
	if err := localfs.EnsureDir(dir); err != nil {
		return err
	}
	lock, err := acquire(filepath.Join(dir, LockFileName))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, lock.Close()) }()
	reg, _, err := load(dir)
	if err != nil {
		return err
	}
	if err = fn(reg); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(dir, FileName, append(data, '\n'))
}

func acquire(path string) (*processlock.Lock, error) {
	deadline := time.Now().Add(lockWait)
	for {
		lock, err := processlock.Acquire(path)
		if !errors.Is(err, processlock.ErrBusy) {
			return lock, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another wasync process is updating agent credentials; retry")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func writeAtomic(dir, name string, data []byte) (result error) {
	suffix, err := newClientID()
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "."+name+".tmp-"+suffix)
	f, err := localfs.OpenFile(tmp)
	if err != nil {
		return err
	}
	defer func() {
		if result != nil {
			os.Remove(tmp)
		}
	}()
	if err = f.Truncate(0); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil // Directory handles cannot be fsynced on Windows; MoveFileEx is durable enough.
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
