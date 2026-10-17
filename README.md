# WhatsappTuiCli

A personal, local WhatsApp group archive with a **full-screen terminal UI**,
written in Go using [whatsmeow](https://github.com/tulir/whatsmeow) and
[Bubble Tea](https://github.com/charmbracelet/bubbletea).

The executable is named **`wasync`**: launch it for the terminal UI, or use
**`wasync query …`** for read-only JSON queries from local agents.

## Build and launch

Requires macOS or Linux, Go 1.27.1 or newer (the project selects Go 1.27.2), and a
C compiler. On macOS, install the Xcode Command Line Tools if needed.

```sh
git clone https://github.com/gehariharan/WhatsappTuiCli.git
cd WhatsappTuiCli
go build -tags sqlite_fts5 -o bin/wasync ./cmd/wasync
./bin/wasync
```

**Launching without a command opens the TUI and starts capture for groups you
already enabled.** Your Phase 1 session and group preferences are reused. The
archive migrates automatically; do not delete your existing data directory.

If no session exists, pairing happens inside the TUI. Open WhatsApp on your phone
and select **Linked Devices → Link a Device**. Resize your terminal if the QR code
needs more space. After authentication, groups are discovered with capture
disabled by default. Enable the groups you want using Space. Enabling automatically
queues a request for older messages from your phone.

Keep the TUI or `wasync serve` running while you want to capture messages. Quitting
stops capture and the local API, and leaves linked-device credentials available
for the next launch. The TUI and headless server expose an authenticated loopback
HTTP API by default.

## TUI controls

| Key | Action |
| --- | --- |
| ↑ / ↓ or j / k | Move through groups or messages |
| Mouse wheel / click | Scroll / select a row; click a group checkbox to toggle capture |
| PgUp / PgDn, Home / End | Jump through the list |
| Space | Enable or disable capture for the selected group |
| Enter | Open the group's archive; in messages, expand the full message |
| / | Filter groups; in the archive, search message text |
| e | Show enabled groups only |
| n / p or → / ← | Older / newer message page |
| R | Return to the latest message page |
| Ctrl+D / Ctrl+U | Scroll the selected message body |
| ↑ / ↓ in expanded message | Scroll the full message body |
| r | Refresh group discovery |
| h | Queue/retry older history for the selected/open group |
| c | Connect or retry pairing |
| D | Disable and purge the selected group's archive, after confirmation |
| L | Unlink the device and quit, after confirmation |
| Esc | Close details, return to groups, or clear the group filter |
| ? | Show keyboard help |
| q or Ctrl+C | Quit; keep the device linked |

Group details show archived counts, last message time, observed history state and
potential account gaps. Enabled groups appear first. The selected group stays
selected when a refresh changes the ordering. The message list uses keyset
pagination; new arrivals do not shift an older page while you browse it. Search
uses a literal FTS5 phrase and is scoped to the open group.

The latest archive page refreshes while you browse it. Older pages stay anchored.
Full message detail works in narrow terminals through Enter. Untrusted group
names, sender labels and message bodies are escaped before terminal rendering.

Browse the archive without connecting:

```sh
./bin/wasync tui --offline
```

`wasync tui` and `wasync run` both open the live TUI. Fullscreen operation has no
three-minute command timeout. The `--timeout` setting still applies to the
scriptable diagnostic commands below.

## Automatic historical download

Whenever you enable a group, wasync automatically queues its historical download.
Groups already enabled when upgrading are queued on the next launch too.

1. Quit the running older binary with **q**, rebuild, and launch `./bin/wasync`.
2. Keep the group enabled and the TUI online. Keep your phone connected to the
   internet so it can respond; opening WhatsApp may help it process the request.
3. Watch **Capture details** for `Older history queued`, `Requesting 50 older
   messages`, and `Waiting for history from your phone`.
4. Received messages enter the archive automatically. The worker asks for the next
   50 older messages while the returned cursor advances. It sends one request at a
   time, with a pause between pages.
5. Press **h** on the group, or while browsing its messages, to queue a retry after
   a timeout or failure. Do not confuse **n/p**, which browse local pages, with
   **h**, which asks the phone for more history.

The request requires a genuine known message ID and timestamp. Wasync uses its
oldest local message, or a metadata-only cursor observed during this running
session. For an empty group with no known cursor, the UI says **Waiting for a
known message**. Once a new message is observed in that group, historical requests
start automatically. A single newly captured message is enough to bootstrap the
first request.

Only message ID, group ID, direction and timestamp cursor metadata may be kept
in RAM for unselected groups; their message bodies are never archived. These
transient cursors are discarded when wasync exits. Selected groups' request
cursors and job states are persisted so pagination can resume after restarting.

Disabling a group stops further requests and ignores its content on delivery.
Empty/repeated pages stop the attempt as **No older page returned (coverage
unknown)**. Failed sends and a two-minute response timeout are shown separately;
no response is not proof of an empty or complete history. Requests are protocol
messages addressed to your own linked device, not chat messages posted to the
selected group.

## What is captured

Only explicitly enabled groups are archived. Disabling a group takes effect at
the database transaction boundary and retains existing records. Purge disables
capture and removes the group's stored records and search entries.

The archive captures text, extended text, captions, sender/group/message IDs,
original timestamps, whether the message was sent by you, reply IDs and safe
media metadata. It does not download attachment files or store thumbnails, quoted
message bodies, download URLs or media decryption keys. Reactions and unsupported
types currently retain minimal type metadata.

Live and historical messages share the same allowlist and normalization rules.
Repeated deliveries are deduplicated by account, group and WhatsApp message ID.
Delivered edits update the current text. Delivered revokes clear the body and
retain tombstones. A revoke arriving before the original prevents a subsequent
history delivery from restoring the body; edits arriving before originals are
held as pending content until the original arrives. Pending edits expire after
30 days, while revocation tombstones are retained.

View-once content is excluded. Ephemeral text is kept only when a usable expiry
can be determined, hidden immediately after expiry and cleared on maintenance
(every ten seconds) or restart. Ephemeral edits without an absolute deadline are
skipped rather than extending their retention from the edit time. Retention does
not remove copies in backups or guarantee forensic erasure of old SQLite pages.

## History and recovery limits

Historical import is **best-effort**. Available history events are processed in
small transactions. Original timestamps are preserved; batch state and durable
chunk counts record progress. An interrupted batch is marked interrupted after
restart. Reported upstream progress, even 100%, does not mean the archive is
complete.

**The history observed during Phase 1 was discarded.** It cannot be imported from
those counters. The new capture path imports any history WhatsApp delivers again;
it neither assumes nor promises that the phone will replay the old batch.
Enabling requests history automatically, but a historical backfill is not guaranteed.

The pinned library handles ordinary network reconnects and the special reconnect
after pairing. Its ordinary retry delay is currently linear. A custom exponential
supervisor remains future hardening work. Reconnects and periods when wasync was
stopped create uncertain account intervals; ending an interval means observation
resumed, not that all missed messages were recovered. Decryption failures for an
enabled group also mark uncertainty.

Selected live messages are written synchronously before our handler reports
success; the library's synchronous acknowledgement option is enabled. A database
write failure stops capture and appears in the TUI. This reduces the time between
delivery and a durable commit, but does not guarantee complete delivery or replay
after every failure. The library's optional decrypted-event buffer is not enabled
because it could retain plaintext for unselected chats.

After a terminal session error, such as being unlinked from the phone, restart
wasync to establish a fresh client and re-pair if needed.

## Pull data into your own database

The archive is local SQLite. You can query it while wasync runs using a read-only
connection (macOS default path shown):

```sh
sqlite3 -readonly "$HOME/Library/Application Support/wasync/archive.db" \
  'SELECT account_id, group_id, message_id, text_content FROM archived_messages LIMIT 20;'
```

A stable view exposes current records:

```sql
SELECT account_id, group_id, message_id, sender_id, timestamp,
       message_type, text_content, edited_at, deleted_at, received_at
FROM archived_messages
ORDER BY timestamp, id;
```

Timestamps are UTC Unix milliseconds. `archived_messages` excludes unmaterialized
pending edits and expired messages. Revoked messages retain their identities and
have empty bodies. Filter by `account_id` and `group_id`; read the database in
read-only mode and keep read transactions short. Never modify schema or archive
tables from external applications.

For a separate DB, use a consistent snapshot and upsert by
`(account_id, group_id, message_id)`, with deletion-aware snapshot reconciliation.
A message-time cursor alone misses edits, late history and purges. A transactional
replication change feed remains later work. The local REST API below provides
read access to the same archive.

## Agent CLI queries

**`wasync` opens the TUI; `wasync query …` reads the archive and exits.** The
examples below use the repository executable; if it is installed on your PATH,
use `wasync` in place of `./bin/wasync`. To install the built executable for
commands from any directory:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 bin/wasync "$HOME/.local/bin/wasync"
export PATH="$HOME/.local/bin:$PATH"
```

Add the PATH export to your shell configuration if needed. Reinstall the binary
after rebuilding.

Keep the TUI capturing in one terminal and query from another:

```sh
./bin/wasync query groups --enabled
./bin/wasync query messages --group '123456789@g.us' --limit 50
./bin/wasync query search "meeting" --group '123456789@g.us'
./bin/wasync query messages --group '123456789@g.us' \
  --since 2026-10-01T00:00:00Z --until 2026-10-02T00:00:00Z \
  --sender '123456789@lid'
./bin/wasync query status
./bin/wasync query --help
```

All query results are JSON on stdout by default; `--json` is also accepted for
explicit agent invocations. Diagnostics go to stderr. Exit codes are 0 for
success (including empty results), 1 for errors and 130 for cancellation. Group
results contain `account_id` and `items`. Message/search results also contain
`next_cursor`. Pass a nonempty cursor as `--cursor 'VALUE'` with the same filters
to retrieve the next page. Page size is 1–200, default 50; newest messages come
first, with stable ordering for equal timestamps. Cursors for the CLI and API
are separate formats. CLI cursors contain no secrets and are scoped to the
current account and filters; they are not an authorization mechanism.

Search uses literal phrase matching with SQLite FTS5. `--group` is required for
`messages` and optional for `search`. `--since` is inclusive and `--until` is
exclusive; timestamps are RFC3339. Query output uses the same message and group
fields as the API. Expired and pending messages are excluded; revoked messages
have empty bodies. Disabled groups retain query access to their existing archive.

Queries open the existing archive in SQLite read-only mode, using normal WAL
coordination to see ongoing commits. They acquire no device lock, open no
WhatsApp connection, require no API/token, run no migrations, and create no
credentials. Missing or incompatible archives produce an actionable error;
start the appropriate version of the TUI to create or migrate the archive.
`--data-dir` and `WASYNC_DATA_DIR` select the same archive as other commands:

```sh
./bin/wasync --data-dir /absolute/path/to/private/wasync query groups
```

`query status` reports persisted archive counts, capture timestamps, history
observations and potential gaps. Its `connection` is always `unknown`: an
unfinished capture run or recent heartbeat cannot prove that capture is currently
running or connected. Querying works with capture stopped, but the archive only
updates while capture is active. Pagination reads the live archive rather than a
frozen snapshot and is not a complete replication feed.

To use the TUI and agent queries without an HTTP listener:

```sh
./bin/wasync --no-api
# In another terminal:
./bin/wasync query groups --enabled
```

## Local REST API

Restart the rebuilt executable to enable the API alongside the TUI:

```sh
./bin/wasync
```

The default address is **http://127.0.0.1:8787**. The TUI header shows the actual
address. Use the numeric address as shown; other Host headers are rejected.
All `/api/*` routes require a bearer token. `/health` is a content-free readiness
check and does not require authentication. The token is generated on first API
startup and reused across restarts, with file permissions 0600.

In another terminal on macOS:

```sh
WASYNC_TOKEN=$(cat "$HOME/Library/Application Support/wasync/api.token")
curl --fail -H "Authorization: Bearer $WASYNC_TOKEN" \
  http://127.0.0.1:8787/api/status
curl --fail -H "Authorization: Bearer $WASYNC_TOKEN" \
  http://127.0.0.1:8787/api/groups
curl --fail -H "Authorization: Bearer $WASYNC_TOKEN" \
  'http://127.0.0.1:8787/api/groups/123456789@g.us/messages?limit=50'
curl --fail -G -H "Authorization: Bearer $WASYNC_TOKEN" \
  --data-urlencode 'q=meeting' \
  http://127.0.0.1:8787/api/messages/search
```

Replace the group ID with one from `/api/groups`. On Linux or with a custom data
directory, read `api.token` from that directory instead. Never put the token in a
URL, commit it, or share it. To rotate it, stop wasync, remove only `api.token`,
and restart. Existing cursors become invalid after rotation.

| Method | Route | Result |
| --- | --- | --- |
| GET | `/health` | Archive readiness (not WhatsApp connectivity) |
| GET | `/api/status` | Connection, counts, attention flag and history uncertainty |
| GET | `/api/groups` | Current account's groups and capture/history state |
| GET | `/api/groups/{id}` | One group |
| POST | `/api/groups/{id}/enable` | Enable capture and queue available history |
| POST | `/api/groups/{id}/disable` | Stop capture; retain archived messages |
| POST | `/api/groups/{id}/history` | Retry best-effort history for an enabled group |
| GET | `/api/groups/{id}/messages` | Newest messages first, with pagination |
| GET | `/api/messages/search?q=keyword` | Literal phrase search across the current account |
| GET | `/api/messages/{id}` | One message by its local numeric row ID |

Message list and search responses contain `account_id`, `items` and
`next_cursor`. Pass the returned cursor as `cursor` to retrieve older messages;
an empty cursor means the end of currently stored results. The cursor is signed
and tied to the account and filters. Keep filters unchanged between pages.
Page size defaults to 50; `limit` accepts 1–200.

Both query routes accept `sender_id`, `from` (inclusive), and `to` (exclusive).
Dates use RFC3339, for example `2026-10-01T00:00:00Z`; URL-encode timezone `+`
signs. Search also accepts `group_id`. Response timestamps are RFC3339 UTC;
SQLite timestamps remain Unix milliseconds. Search uses SQLite FTS5. Expired
messages are excluded and revoked messages have empty bodies, just as in the TUI.
Disabling capture preserves the archive and query access. Reads are scoped to the
current account; pairing another account does not expose the old archive via API.

Query results are live archive reads. Pagination is not a frozen snapshot;
new history, edits, expiry and purge can change results. This is not a complete
replication feed. For another database, use snapshot reconciliation and the
composite identity described above; do not use message timestamps as a change log.

### Headless mode and configuration

Quit the TUI before starting another process for the same data directory:

```sh
./bin/wasync serve                    # Capture + API; requires an existing paired session
./bin/wasync serve --offline          # API for existing archive, no WhatsApp connection
./bin/wasync --api-addr 127.0.0.1:8788 # TUI + API on another local port
./bin/wasync --no-api                 # TUI with the API disabled
```

`serve` runs until Ctrl-C or SIGTERM and shuts down the API before closing the
archive. `--timeout` does not limit service lifetime. `WASYNC_API_ADDR` supplies
the default bind address; `--api-addr` overrides it. Only literal IPv4/IPv6
loopback addresses are accepted. API responses use `Cache-Control: no-store`;
foreign browser origins are rejected and no CORS access is enabled. Keep the API
local for this MVP. All routes share the running process's archive and device
owner, without creating a second WhatsApp session.

## Files, configuration and privacy

All commands use the same private data directory, normally:

- macOS: `~/Library/Application Support/wasync`
- Linux: `$XDG_CONFIG_HOME/wasync` or `~/.config/wasync`

Override it using `WASYNC_DATA_DIR` or the global `--data-dir` flag; flags win.
Global flags precede the command.

```sh
./bin/wasync --data-dir "$PWD/.local/wasync"
WASYNC_DATA_DIR="$PWD/.local/wasync" ./bin/wasync
```

The directory is created with 0700 permissions, and managed files with 0600.
Shared existing directories and symlink data files are rejected. Use a dedicated
location owned by your OS user with trusted parent directories. No credentials,
QR payloads or message bodies are written to logs. Filesystem permissions do not
encrypt data at rest.

| File | Purpose |
| --- | --- |
| `session.db` | Whatsmeow credentials and protocol state; protect as a secret |
| `archive.db` | Accounts, groups, messages, FTS5, sync progress, history jobs and gap records |
| `api.token` | Local API bearer token; keep private |
| `wasync.log` | Structured lifecycle logs from TUI sessions |
| `device.lock` | Process ownership lock, retained after exit |
| `*-wal`, `*-shm` | SQLite WAL sidecars when present |

Only one wasync process owns a data directory. Manage groups in the running TUI
or through its API. `query` commands can read the archive concurrently; quit the
owner before using other CLI commands against that directory. The OS releases the
lock after crashes. Never delete the lock file to bypass an active process or run
copies of the same device credentials concurrently.

Both databases use WAL, foreign keys, a busy timeout and synchronous FULL. Stop
wasync before a file-copy backup or use SQLite's backup API. Copying only the live
`.db` file can omit WAL contents. Keep session files, archives and backups private
and outside Git.

## Scriptable diagnostics

These commands remain useful for setup and scripting. They do not start capture:

```sh
./bin/wasync login --select=false
./bin/wasync groups list
./bin/wasync groups list --refresh --json
./bin/wasync groups enable '123456789@g.us'
./bin/wasync groups disable '123456789@g.us'
./bin/wasync status
./bin/wasync --timeout 5m login --select=false --observe 60s
./bin/wasync logout
```

Cached list and preference commands open no WhatsApp connection. `--refresh`
connects only to refresh discovery. `status` inspects local state; it cannot tell
whether the saved device was revoked remotely. Observation counts all received
events but discards bodies. Logout asks WhatsApp to unlink the device, preserves
archived data, and reports a failed remote unlink as an error.

## Validation

```sh
go test -tags sqlite_fts5 ./...
go test -race -tags sqlite_fts5 ./...
go vet -tags sqlite_fts5 ./...
```

Tests use synthetic events and temporary files. They cover Phase 1 migration,
consent checks at commit time, live/history deduplication, account isolation,
original timestamps, out-of-order edits/revokes, search index updates, expiry,
purge, pagination, interrupted batch state, process locks, private files,
terminal controls, filtering, keyboard actions, narrow terminal rendering, automatic
history queueing, cursor encoding, response correlation, pagination, timeout and
cancellation behavior. API tests cover authentication, loopback binding, origin checks,
account isolation, signed pagination, date/sender filters, concurrent reads during
capture, edited/revoked/expired/purged content, and headless shutdown. CLI query
tests cover reads while the capture lock is held, active WAL visibility, pagination,
filters, account isolation, missing archives, read-only enforcement, schema checks,
and default TUI dispatch. History request tests use an injected sender; they do not
send protocol requests to a real account.

Manual account checks:

1. Launch wasync and confirm it reuses your paired session and enabled group.
2. Have someone send a text in an enabled and a disabled group. Only the enabled
   group's message should appear in the archive. No outgoing messages are sent by
   wasync.
3. Toggle capture with Space; confirm subsequent messages follow the preference.
4. Browse with Enter, search with /, and scroll long messages with Enter then ↑/↓.
5. Restart and verify the archive and preferences persist without duplicate rows.
6. Interrupt and restore the network; verify the UI reports reconnection and
   uncertainty. History may or may not be delivered.
7. Test purge on disposable archived data; confirm the group is disabled and its
   messages disappear from browsing and search.

Real account delivery and historical completeness cannot be established by the
synthetic tests. Do not re-pair simply to obtain history without understanding
that replay is not guaranteed.

This is an unofficial WhatsApp client: protocol compatibility, WhatsApp terms and
account restriction risks apply. Obtain appropriate permission before archiving
or sharing conversations. There are no features for posting messages to conversations, cloud services,
AI processing, attachment downloads or application web frontend in this phase.
The original design and risk review are saved in
[the HTML design artifact](.lavish/whatsapp-sync-design.html).
