#!/usr/bin/env bash
# Enrol this Linux machine as an OpenClaw node so agents can work on it through
# ClawHQ. Paste the command ClawHQ gives you; it carries a one-time code.
#
#   curl -fsSL https://raw.githubusercontent.com/surpriseawofemi/clawHQ/main/scripts/node.sh | bash -s -- --code <code>
#
# Options:
#   --code <setup code>     required; minted by ClawHQ, valid for a few minutes
#   --version <x.y.z>       OpenClaw version to install (default: the gateway's)
#   --name <display name>   how the machine shows up (default: hostname)
#   --mode auto|semi|manual auto: agents run anything here without asking;
#                           semi (default): safe reads run, the rest asks you in ClawHQ;
#                           manual: every command asks
#   --node-path <dir>       use the Node.js under <dir>/bin instead of installing one
#
# What it does: makes sure a Node.js 24 is available (the system one if it is 24,
# otherwise an official build unpacked into /opt/node24 that leaves the system Node
# and anything running on it alone), installs the OpenClaw CLI under that Node,
# writes the exec policy for the chosen mode, then pairs with the gateway and
# installs the node host as a system service using that Node's absolute path.
set -euo pipefail

CODE=""
VERSION="2026.9.4"
NAME="$(hostname -s 2>/dev/null || hostname)"
MODE="semi"
NODE_PATH_DIR=""
while [ $# -gt 0 ]; do
  case "$1" in
    --code) CODE="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --mode) MODE="$2"; shift 2 ;;
    --allow-writes) MODE="manual"; shift ;;
    --node-path) NODE_PATH_DIR="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done
[ -n "$CODE" ] || { echo "--code is required (ClawHQ → Settings → Machines → Add a machine)" >&2; exit 2; }

say() { printf '\033[1;36m» %s\033[0m\n' "$*"; }
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  if command -v sudo >/dev/null 2>&1; then SUDO="sudo"; fi
fi

# ---- Node.js ----------------------------------------------------------------
# OpenClaw needs Node.js 24 (or 26.1+); 25 is refused. A production box often runs
# its apps on an older system Node, so that one is never replaced: if it will not
# do, an official build goes to /opt/node24 and only OpenClaw uses it.
node_ok() { # $1 = node binary
  local major
  major="$("$1" -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0)"
  [ "$major" -ge 24 ] && [ "$major" -ne 25 ]
}
NODE_BIN=""
if [ -n "$NODE_PATH_DIR" ]; then
  node_ok "$NODE_PATH_DIR/bin/node" || { echo "$NODE_PATH_DIR/bin/node is not a usable Node.js 24" >&2; exit 1; }
  NODE_BIN="$NODE_PATH_DIR/bin"
elif command -v node >/dev/null 2>&1 && node_ok "$(command -v node)"; then
  NODE_BIN="$(dirname "$(command -v node)")"
elif [ -x /opt/node24/bin/node ] && node_ok /opt/node24/bin/node; then
  NODE_BIN="/opt/node24/bin"
else
  if command -v node >/dev/null 2>&1; then
    say "System Node.js $(node -v) stays as it is; installing Node.js 24 beside it in /opt/node24"
  else
    say "No Node.js found; installing Node.js 24 in /opt/node24"
  fi
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64) NARCH="x64" ;;
    aarch64|arm64) NARCH="arm64" ;;
    *) echo "unsupported CPU: $ARCH" >&2; exit 1 ;;
  esac
  # Latest 24.x from the official index, so no third-party apt repo is involved.
  NVER="$(curl -fsSL https://nodejs.org/dist/index.json | grep -o '"version":"v24\.[0-9]*\.[0-9]*"' | head -1 | cut -d'"' -f4)"
  [ -n "$NVER" ] || { echo "could not read the Node.js release list from nodejs.org (DNS or network?)" >&2; exit 1; }
  TARBALL="node-$NVER-linux-$NARCH.tar.xz"
  TMP="$(mktemp -d)"
  curl -fsSL "https://nodejs.org/dist/$NVER/$TARBALL" -o "$TMP/$TARBALL"
  curl -fsSL "https://nodejs.org/dist/$NVER/SHASUMS256.txt" -o "$TMP/SHASUMS256.txt"
  (cd "$TMP" && grep " $TARBALL\$" SHASUMS256.txt | sha256sum -c - >/dev/null) || { echo "checksum mismatch for $TARBALL" >&2; exit 1; }
  $SUDO mkdir -p /opt/node24
  $SUDO tar -xJf "$TMP/$TARBALL" -C /opt/node24 --strip-components=1
  rm -rf "$TMP"
  NODE_BIN="/opt/node24/bin"
fi
export PATH="$NODE_BIN:$PATH"
say "Node.js $(node -v) at $NODE_BIN"

# ---- OpenClaw CLI --------------------------------------------------------------
CURRENT="$(openclaw --version 2>/dev/null | sed -nE 's/^OpenClaw ([0-9.]+).*/\1/p' || true)"
if [ "$CURRENT" = "$VERSION" ]; then
  say "OpenClaw $VERSION already installed"
else
  say "Installing OpenClaw $VERSION"
  PREFIX="$(npm config get prefix 2>/dev/null || echo /usr/local)"
  if [ -w "$PREFIX/lib" ] 2>/dev/null; then
    npm install -g "openclaw@$VERSION"
  else
    $SUDO env PATH="$PATH" npm install -g "openclaw@$VERSION"
  fi
fi
command -v openclaw >/dev/null 2>&1 || { echo "openclaw is not on PATH after install; expected it under $NODE_BIN" >&2; exit 1; }

# ---- exec policy: auto, semi-auto or manual ---------------------------------------
# The same file the gateway pushes with `exec.approvals.node.set`; ClawHQ's
# Machines page can change the mode later without touching this file by hand.
STATE_DIR="${OPENCLAW_STATE_DIR:-$HOME/.openclaw}"
mkdir -p "$STATE_DIR"
POLICY="$STATE_DIR/exec-approvals.json"
case "$MODE" in
  auto|semi|manual) ;;
  *) echo "--mode must be auto, semi or manual (got \"$MODE\")" >&2; exit 2 ;;
esac
if [ -f "$POLICY" ]; then
  say "Keeping the existing exec policy at $POLICY (mode can be changed from ClawHQ)"
elif [ "$MODE" = "auto" ]; then
  say "Auto mode: agents run anything on this machine without asking"
  cat > "$POLICY" <<'EOF'
{ "version": 1, "defaults": { "security": "full", "ask": "off" }, "agents": {} }
EOF
elif [ "$MODE" = "manual" ]; then
  say "Manual mode: every command asks you in ClawHQ"
  cat > "$POLICY" <<'EOF'
{ "version": 1, "defaults": { "security": "allowlist", "ask": "on-miss", "askFallback": "deny" }, "agents": {} }
EOF
else
  say "Semi-auto mode: reads run without asking, everything else asks you in ClawHQ"
  SAFE="cat head tail less grep wc ls find stat du df free uptime uname hostname whoami id env ps top pgrep lsof ss netstat dig nslookup curl journalctl systemctl docker git node npm nginx"
  {
    echo '{'
    echo '  "version": 1,'
    echo '  "defaults": { "security": "allowlist", "ask": "on-miss", "askFallback": "deny" },'
    echo '  "agents": { "*": { "security": "allowlist", "ask": "on-miss", "askFallback": "deny", "allowlist": ['
    first=1
    for p in $SAFE; do
      for dir in /usr/bin /bin /usr/local/bin; do
        if [ "$first" = 1 ]; then first=0; else echo ','; fi
        printf '    { "pattern": "%s/%s" }' "$dir" "$p"
      done
    done
    echo
    echo '  ] } }'
    echo '}'
  } > "$POLICY"
fi

# ---- pair and install the service -------------------------------------------------
say "Pairing with the gateway as \"$NAME\" and installing the node service"
openclaw connect --service --display-name "$NAME" "$CODE"
say "Done. Approve the pairing in ClawHQ (Settings → Machines) if it is not approved already."
if [ "$NODE_BIN" = "/opt/node24/bin" ]; then
  say "The node service runs on /opt/node24; to use the CLI yourself: export PATH=/opt/node24/bin:\$PATH"
fi
