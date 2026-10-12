#!/usr/bin/env bash
# Rebuilds lab/sfu from upstream at the pinned commit and checks the result byte for byte.
#
# The vendored SFU is upstream pion sfu-ws with a small ICE patch. A patch file that no longer
# applies, or silently applies to a different upstream, is worse than no patch file: it looks like
# provenance and is not. This runs on every push, and needs network.
set -euo pipefail
cd "$(dirname "$0")/.."

commit=$(sed -n 's/.*upstream commit \([0-9a-f]\{7,40\}\).*/\1/p' lab/sfu/README.md | head -1)
[ -n "$commit" ] || { echo "no pinned upstream commit in lab/sfu/README.md"; exit 1; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "https://raw.githubusercontent.com/pion/example-webrtc-applications/$commit/sfu-ws/main.go" -o "$tmp/main.go"
( cd "$tmp" && patch -p1 --silent < "$OLDPWD/lab/sfu/patches/0001-ice-config.patch" )

for f in main.go ice.go; do
  if ! diff -q "$tmp/$f" "lab/sfu/$f" >/dev/null; then
    echo "lab/sfu/$f does not match upstream@$commit plus the committed patch"
    diff -u "$tmp/$f" "lab/sfu/$f" | head -40
    exit 1
  fi
done
echo "lab/sfu matches upstream@${commit:0:12} plus patches/0001-ice-config.patch"
