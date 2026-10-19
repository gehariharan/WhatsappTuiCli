# Threat model: agent access to wasync

Scope: the local wasync owner (TUI or `serve`), its loopback API, the `wasync query` CLI, and agent clients such as whatsapp-axi. See [ADR-001](ADR-001-repository-boundaries.md).

## Assets

- WhatsApp pairing credentials (whatsmeow session store)
- The SQLite archive: message bodies, media, chat membership, capture preferences
- Ability to act as the user on WhatsApp: send, react, edit, revoke, change chat or group state
- Legacy tokens (`api.token`, `send.token`, `approve.token`) and agent credentials

## Trust boundaries

| Boundary | Enforcement |
|---|---|
| Other machines → API | Literal loopback address only (`api.ValidateAddr`); Host and Origin checks against DNS rebinding and browsers |
| Other local users → data dir | Private file and directory permissions (`internal/localfs`, Unix modes and Windows ACLs) |
| Agent → wasync | Scoped agent credential checked by wasync on every `/api/v1` request; scopes, chat allowlist, rate limit, expiry; audit log |
| Agent → destructive actions | Not grantable; human approval queue with typed confirmation |
| Same OS user → archive | Not a boundary. `wasync query` offline reads are equivalent to the user reading their own files and are read-only |
| WhatsApp content → agent | Message text is untrusted data; never rendered as instructions or hints |

## Threats and mitigations

| Threat | Mitigation |
|---|---|
| Agent instructed by message content (prompt injection) to send or delete | Writes need explicit scopes and allowlisted chats granted by a human in wasync; destructive actions are never grantable; AXI renders content as delimited data |
| Agent or skill passes flags to bypass confirmation | Flags are UX only; wasync enforces scopes server-side |
| Leaked agent credential | Hash-only storage, per-client revoke and expiry, rate limits, audit trail |
| Agent credential replayed on legacy routes, or legacy token on v1 | The two credential types are isolated; each route family accepts only its own |
| Legacy read token mutating capture policy (existing behaviour) | Recorded in the baseline audit; unchanged for compatibility; agent credentials do not inherit it |
| Duplicate WhatsApp session | Only wasync pairs; `device.lock`; AXI never connects to WhatsApp |
| Data loss on upgrade | Additive-only migrations, automatic pre-migration backup, schema upgrade tests from every released schema |
| Secrets or bodies in logs | Audit and diagnostics record identifiers and outcomes only |
| Change feed skipping events | Transactional journal with sequence cursors and an epoch; no timestamp cursors |
| Webhook SSRF and data egress | Deferred; opt-in, HTTPS, blocked private ranges, HMAC, no bodies by default |
