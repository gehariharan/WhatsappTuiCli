// Package policy records the dependency boundary from ADR-001: whatsapp-axi
// never links WhatsApp protocol code or a SQLite driver, and never imports
// wasync internals. policy_test.go enforces it.
package policy

// ForbiddenModules are substrings that must never appear in go.mod, go.sum
// or an import path.
var ForbiddenModules = []string{
	"whatsmeow",
	"go.mau.fi",
	"sqlite",
	"whatsappsync",
	"database/sql",
}
