#!/usr/bin/env bash
# Install the pinned OpenCode 2 CLI for a source/native deployment.
#
# The platform image and the built-in generic-backend Dockerfile already ship
# it. This script covers a checkout that runs the platform binary directly
# (scripts/dev.sh) and execution machines that do not use the image.
set -euo pipefail

VERSION="${OPENCODE_VERSION:-2.3.0}"
PREFIX="${OPENCODE_INSTALL_PREFIX:-$HOME/.local}"

opencode_major() {
  command -v opencode >/dev/null 2>&1 || return 0
  opencode --version 2>/dev/null | sed -n 's/^[^0-9]*\([0-9][0-9]*\).*/\1/p' | head -1
}

existing="$(opencode_major)"
if [[ -n "$existing" && "$existing" -ge 2 ]]; then
  echo "OpenCode v2 already available: $(command -v opencode)"
  exit 0
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "OpenCode ${VERSION} needs npm; install Node.js and rerun this script." >&2
  exit 1
fi

if [[ -n "$existing" ]]; then
  echo "OpenCode v${existing} is installed; installing v2 at ${PREFIX} ..."
else
  echo "Installing OpenCode ${VERSION} at ${PREFIX} ..."
fi

mkdir -p "$PREFIX/bin"
npm install --global --prefix "$PREFIX" --no-audit --no-fund "@opencode/cli@${VERSION}"

# npm 12 blocks lifecycle scripts unless the package is allowlisted. OpenCode
# then leaves a stub that exits with "postinstall script was not run", so run
# the same postinstall explicitly. It copies the platform binary from the
# optional dependency that npm already installed.
postinstall="$PREFIX/lib/node_modules/@opencode/cli/postinstall.mjs"
if [[ -f "$postinstall" ]]; then
  (cd "$(dirname "$postinstall")" && node "$postinstall")
fi

export PATH="$PREFIX/bin:$PATH"
hash -r 2>/dev/null || true
installed="$(opencode_major)"
if [[ -z "$installed" || "$installed" -lt 2 ]]; then
  echo "OpenCode ${VERSION} did not install a working v2 binary at ${PREFIX}/bin/opencode." >&2
  exit 1
fi
echo "Installed OpenCode v2: $(command -v opencode) ($(opencode --version 2>/dev/null | head -1))"
