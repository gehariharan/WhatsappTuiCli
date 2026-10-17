#!/usr/bin/env bash
# Maintainer tool: build a native binary, archive, and macOS installer.
set -euo pipefail

version="${1:-}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9][A-Za-z0-9.-]*)?$ ]]; then
  echo "Usage: scripts/package.sh VERSION (example: 0.1.0-preview.1)" >&2
  exit 1
fi
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$project_dir"
platform="$(go env GOHOSTOS)"
architecture="$(go env GOHOSTARCH)"
case "$platform/$architecture" in
  darwin/arm64|darwin/amd64|linux/arm64|linux/amd64) ;;
  *) echo "Unsupported native release target: $platform/$architecture" >&2; exit 1 ;;
esac
out="$project_dir/dist/$version/${platform}_${architecture}"
mkdir -p "$out"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
commit="$(git rev-parse --short=12 HEAD)"
if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then commit="$commit-dirty"; fi
built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
if [[ "$platform" == darwin ]]; then export MACOSX_DEPLOYMENT_TARGET=15.0; fi
CGO_ENABLED=1 GOOS="$platform" GOARCH="$architecture" go build -trimpath -tags sqlite_fts5 \
  -ldflags "-s -w -X whatsappsync/internal/buildinfo.Version=$version -X whatsappsync/internal/buildinfo.Commit=$commit -X whatsappsync/internal/buildinfo.BuiltAt=$built_at" \
  -o "$stage/wasync" ./cmd/wasync
if [[ "$platform" == darwin ]]; then
  if [[ -n "${WASYNC_CODESIGN_IDENTITY:-}" ]]; then
    codesign --force --options runtime --timestamp --sign "$WASYNC_CODESIGN_IDENTITY" "$stage/wasync"
  else
    codesign --force --sign - "$stage/wasync"
  fi
fi
"$stage/wasync" version
cp README.md "$stage/README.md"
python3 scripts/collect-licenses.py "$stage/THIRD_PARTY_LICENSES"
name="wasync_${version}_${platform}_${architecture}"
tar -czf "$out/$name.tar.gz" -C "$stage" wasync README.md THIRD_PARTY_LICENSES

if [[ "$platform" == darwin ]]; then
  mkdir -p "$stage/payload/usr/local/bin" "$stage/payload/usr/local/share/wasync" "$stage/hooks"
  cp -R "$stage/THIRD_PARTY_LICENSES" "$stage/payload/usr/local/share/wasync/"
  install -m 755 "$stage/wasync" "$stage/payload/usr/local/bin/wasync"
  # Refuse installation on the wrong CPU rather than installing an unusable CLI.
  if [[ "$architecture" == arm64 ]]; then expected_cpu=arm64; else expected_cpu=x86_64; fi
  cat > "$stage/hooks/preinstall" <<HOOK
#!/bin/sh
set -eu
if [ "\$(uname -m)" != "$expected_cpu" ]; then
  echo "Download the wasync installer matching this Mac's CPU ($expected_cpu required)." >&2
  exit 1
fi
major="\$(sw_vers -productVersion | cut -d. -f1)"
if [ "\$major" -lt 15 ]; then
  echo "This wasync package requires macOS 15 or later." >&2
  exit 1
fi
if [ -L /usr/local/bin/wasync ]; then
  echo "A symlink already owns /usr/local/bin/wasync. Uninstall that installation before using this package." >&2
  exit 1
fi
HOOK
  chmod 755 "$stage/hooks/preinstall"
  pkg_args=(--root "$stage/payload" --scripts "$stage/hooks"
    --identifier com.gehariharan.wasynctuicli --version "${version%%-*}"
    --install-location / --ownership recommended)
  if [[ -n "${WASYNC_INSTALLER_IDENTITY:-}" ]]; then pkg_args+=(--sign "$WASYNC_INSTALLER_IDENTITY" --timestamp); fi
  pkgbuild "${pkg_args[@]}" "$out/$name.pkg"
fi
(
  cd "$out"
  shasum -a 256 "$name.tar.gz" > "$name.tar.gz.sha256"
  if [[ -f "$name.pkg" ]]; then shasum -a 256 "$name.pkg" > "$name.pkg.sha256"; fi
)
printf 'Release packages: %s\n' "$out"
