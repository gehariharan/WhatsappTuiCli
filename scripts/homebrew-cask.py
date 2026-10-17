#!/usr/bin/env python3
"""Generate a binary-only Homebrew cask from both native macOS archives."""
import hashlib
from pathlib import Path
import re
import sys

if len(sys.argv) != 4 or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9.-]*)?", sys.argv[1]):
    raise SystemExit("Usage: scripts/homebrew-cask.py VERSION ASSET_DIRECTORY OUTPUT.rb")
version, directory, output = sys.argv[1:]
hashes = {}
for architecture in ("arm64", "amd64"):
    artifact = Path(directory) / f"wasync_{version}_darwin_{architecture}.tar.gz"
    hashes[architecture] = hashlib.sha256(artifact.read_bytes()).hexdigest()
Path(output).write_text(f'''# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "{version}"
  sha256 arm:   "{hashes['arm64']}",
         intel: "{hashes['amd64']}"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{{version}}/wasync_#{{version}}_darwin_#{{arch}}.tar.gz"
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: ">= :sequoia"

  binary "wasync"
end
''', encoding="utf-8")
