# wasync ↔ whatsapp-axi interface contract


Locked by ADR-001 ([ADR-001](ADR-001-repository-boundaries.md)). Summary of what each side must provide:

### Transports (ADR D-1)

| Transport | When | Allowed |
|---|---|---|
| Local API `/api/v1` with a scoped agent credential | owner running and version compatible | reads, `observe`, checkpoint commits, and later the mutations the grant allows |
| `wasync query … --json` subprocess | no owner listening | reads and journal reads only; never checkpoint commits or mutations |

AXI never touches WhatsApp, the SQLite archive, the session store or `device.lock`, and never imports `internal/`. Each AXI result states which transport served it.

### Authorization (ADR D-2)

- Scoped agent clients created only by a human in wasync (`wasync agents grant|list|revoke`, interactive terminal or TUI).
- Scopes: `read.chats`, `read.messages`, `read.media`, `observe`, `send`, `react`, `edit.own`, `chat.state`, `group.admin`, `capture.manage`, `export`. Destructive operations are not grantable and stay on the human approval queue.
- Chat allowlists, rate limits and expiry enforced in wasync handlers. Audit log of every v1 mutation attempt without bodies or tokens.
- `GET /api/v1/whoami` returns the client name, scopes and constraints so AXI and `doctor` can explain a denial (`error_code: forbidden`, `help:` names the missing scope and the `wasync agents grant` command a human must run).
- Legacy `/api/*` routes and tokens unchanged.

### Version handshake

`GET /api/v1/version` (no auth, loopback only, no content) and offline `wasync query version --json`:

```json
{"api_version": 1, "api_minor": 0, "wasync_version": "0.1.0-preview.…", "schema_version": 6,
 "features": ["chats","messages","search","search.context","thread","message.ref","health.deep",
              "auth.scopes","changes","checkpoints","diagnostics"]}
```

The embedded [AXI compatibility matrix](../../axi/internal/compat/compatibility.json)
owns the minimum API version and required features per command; a missing feature
disables that command with `wasync_too_old` and an upgrade hint.

### Response envelope (v1 only)

Success: `{"request_id", "data", "page": {"next_cursor", "has_more"}}`
Error: `{"request_id", "error_code", "error", "help", "state"}`

Error codes: `unauthorized`, `forbidden`, `not_found`, `invalid_argument`, `invalid_cursor`, `cursor_expired`, `chat_ambiguous`, `owner_offline`, `no_account`, `archive_unavailable`, `schema_incompatible`, `rate_limited`, `internal`. The `wasync query --v1` mode emits the same envelope so AXI parses both transports identically.

### Identifiers

- Message ref: opaque `m1_<base64url(account_jid \x00 chat_jid \x00 message_id)>`, account-scoped, stable across restarts, purge and re-capture, no migration.
- Chat id: WhatsApp JID. Name lookup returns `chat_ambiguous` with candidates rather than guessing.
- Change cursor: opaque `(account_jid, journal_epoch, sequence)`. Never a timestamp or row ID.
- UTC RFC 3339 at the boundary.

### Endpoints and their offline equivalents

| v1 endpoint | `wasync query` equivalent | AXI command | Scope |
|---|---|---|---|
| `GET /api/v1/version` | `query version` | handshake, `doctor` | none |
| `GET /api/v1/whoami` | – (offline is unscoped) | `doctor`, denials | any |
| `GET /api/v1/diagnostics` | `query doctor` | `doctor` | any |
| `GET /api/v1/health/deep` | `query status --v1` | `status`, home view | `read.chats` |
| `GET /api/v1/chats[?match=]` | `query chats --v1 [--match]` | `chats` | `read.chats` |
| `GET /api/v1/chats/{jid}/messages` | `query messages --v1` | `messages CHAT` | `read.messages` |
| `GET /api/v1/messages/search?context=` | `query search --v1 --context` | `search` | `read.messages` |
| `GET /api/v1/messages/{ref}` | `query message --ref` | `message` | `read.messages` |
| `GET /api/v1/messages/{ref}/thread` | `query thread --ref` | `thread` | `read.messages` |
| `GET /api/v1/changes?after=&limit=` | `query changes --after` | `observe` | `observe` |
| `GET/PUT /api/v1/checkpoints/{consumer}` | read only | `observe --consumer`, `observe ack` | `observe` |

### Message content safety

Message text is data in a typed field. AXI marks it as untrusted content in output and never turns it into hints or instructions.
