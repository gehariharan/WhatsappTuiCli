#!/usr/bin/env bash
set -euo pipefail
version="${1:?Usage: scripts/verify-signed-release.sh VERSION ARCHITECTURE}"
architecture="${2:?Usage: scripts/verify-signed-release.sh VERSION ARCHITECTURE}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9][A-Za-z0-9.-]*)?$ ]]; then
  echo 'Invalid version' >&2; exit 1
fi
case "$architecture" in arm64) cpu=arm64;; amd64) cpu=x86_64;; *) exit 1;; esac
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
if [[ "$(gh release view "v$version" --json isDraft --jq .isDraft)" != true ]]; then
  echo 'Only draft releases may be published by this workflow.' >&2; exit 1
fi
name="wasync_${version}_darwin_${architecture}"
gh release download "v$version" --pattern "$name.*" --pattern checksums.txt --dir "$work"
cd "$work"
# Require the aggregate checksum manifest to cover both assets.
python3 - "$name" <<'PY'
import hashlib, sys
from pathlib import Path
lines = Path('checksums.txt').read_text().splitlines()
entries = {line.split()[1].removeprefix('./'): line.split()[0] for line in lines}
for suffix in ('.tar.gz', '.pkg'):
    name = sys.argv[1] + suffix
    if entries.get(name) != hashlib.sha256(Path(name).read_bytes()).hexdigest():
        raise SystemExit('Missing or invalid checksum: ' + name)
PY
mkdir archive
tar -xzf "$name.tar.gz" -C archive
[[ "$(lipo -archs archive/wasync)" == "$cpu" ]]
codesign --verify --strict \
  -R='anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = "57V379V642"' archive/wasync
pkgutil --check-signature "$name.pkg" > package-signature.txt
grep -Fq 'Developer ID Installer: Xaurum Fintech Private Limited (57V379V642)' package-signature.txt
xcrun stapler validate "$name.pkg"
spctl --assess --type install --verbose=2 "$name.pkg"
codesign --verify --strict --verbose=2 -R='notarized' --check-notarization archive/wasync
pkgutil --expand-full "$name.pkg" expanded
cmp archive/wasync expanded/Payload/usr/local/bin/wasync
# Exercise the exact signed executable with a quarantine attribute intact.
xattr -w com.apple.quarantine "0083;$(printf '%x' "$(date +%s)");wasync-release-check;" archive/wasync
python3 "$project_dir/scripts/smoke-installed.py" "$work/archive/wasync" "$version" "$architecture"
