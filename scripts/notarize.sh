#!/usr/bin/env bash
# Signatures must already exist. Credentials stay in the signing Mac's Keychain.
set -euo pipefail
if [[ $# != 3 ]]; then
  echo 'Usage: scripts/notarize.sh KEYCHAIN_PROFILE SIGNED_BINARY SIGNED_PKG' >&2
  exit 1
fi
profile="$1"
binary="$2"
package="$3"
notary_args=(--keychain-profile "$profile")
if [[ -n "${WASYNC_SIGNING_KEYCHAIN:-}" ]]; then
  notary_args+=(--keychain "$WASYNC_SIGNING_KEYCHAIN")
fi
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Reject development and ad hoc signatures before contacting Apple's service.
codesign --verify --strict \
  -R='anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists' "$binary"
pkgutil --check-signature "$package" > "$work/package-signature.txt"
if ! /usr/bin/grep -q 'Developer ID Installer:' "$work/package-signature.txt"; then
  echo 'The package needs a Developer ID Installer signature.' >&2
  exit 1
fi

submit() {
  local input="$1"
  local report="$2"
  xcrun notarytool submit "$input" "${notary_args[@]}" \
    --wait --timeout 20m --output-format json > "$report"
  python3 - "$report" <<'PY'
import json,sys
from pathlib import Path
report=json.loads(Path(sys.argv[1]).read_text())
if report.get('status') != 'Accepted':
    raise SystemExit('Apple notarization was not accepted. Submission: '+str(report.get('id','unknown')))
print('Apple notarization accepted:',report['id'])
PY
}

# Mach-O executables and tarballs cannot hold stapled tickets. Submit a ZIP of
# the exact signed executable so Apple's online lookup recognizes its code hash.
ditto -c -k --keepParent "$binary" "$work/wasync.zip"
submit "$work/wasync.zip" "$work/binary-notarization.json"
submit "$package" "$work/package-notarization.json"
xcrun stapler staple "$package"
xcrun stapler validate "$package"
spctl --assess --type install --verbose=2 "$package"
echo 'Notarization accepted and installer ticket stapled. Generate package checksums after this step.'
