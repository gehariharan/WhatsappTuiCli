// Package buildinfo holds the whatsapp-axi version. It is a leaf package
// (no imports) so the --version fast path stays cheap. Release builds set
// Version with -ldflags "-X github.com/gehariharan/whatsapp-axi/internal/buildinfo.Version=...".
package buildinfo

// Version is the whatsapp-axi version.
var Version = "0.0.0-dev"
