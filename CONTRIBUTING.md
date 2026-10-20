# Contributing to wasync

This repository contains the wasync TUI/server/CLI, the whatsapp-axi agent CLI,
and the Homebrew definitions. The two binaries have independent Go modules in
one `go.work` workspace. Use Go 1.27.2 and a C compiler; `mise exec go@1.27.2 --`
can run the commands below without changing your global Go installation.

Run `bash scripts/check.sh` for both modules' tests, vet checks, packaging helper
checks, and a real API/offline integration check using a disposable synthetic
archive. It never reads a user's WhatsApp data or connects to WhatsApp.

Keep the process boundary: wasync owns WhatsApp, SQLite, migrations, media and
authorization. AXI calls its versioned loopback API or the read-only `wasync query`
subprocess. AXI must not import WhatsApp libraries or the core's internal packages.
See [the AXI contribution guide](axi/CONTRIBUTING.md).

Both binaries ship from one release pipeline. Release automation updates the
root `Formula/` and `Casks/` directories after publishing verified assets. Do not
change published checksums or point a formula at an unpublished release. See
[releasing](docs/releasing.md) and [the consolidation decision](docs/architecture/ADR-002-monorepo.md).
