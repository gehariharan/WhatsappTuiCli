// Package skills embeds the agent skills shipped in this repository, so
// `whatsapp-axi setup skill` installs exactly the file under skills/ and
// `whatsapp-axi doctor` can tell when an installed copy is stale.
package skills

import _ "embed"

//go:generate go run ../cmd/generate-skill --output whatsapp-axi/SKILL.md

// WhatsappAXI is skills/whatsapp-axi/SKILL.md.
//
//go:embed whatsapp-axi/SKILL.md
var WhatsappAXI []byte
