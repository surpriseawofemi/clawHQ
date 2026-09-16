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
#   --allow-writes          skip the read-only policy; every command asks instead
#
# What it does: checks Node.js 22+, installs the OpenClaw CLI, writes a read-only
# exec policy (reads run without asking, anything else asks you in ClawHQ), then
# pairs with the gateway and installs the node host as a system service.
set -euo pipefail

CODE=""
VERSION="2026.9.4"
NAME="$(hostname -s 2>/dev/null || hostname)"
ALLOW_WRITES=0
while [ $# -gt 0 ]; do
  case "$1" in
    --code) CODE="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --allow-writes) ALLOW_WRITES=1; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done
[ -n "$CODE" ] || { echo "--code is required (ClawHQ → Settings → This machine → Add a machine)" >&2; exit 2; }

say() { printf '\033[1;36m» %s\033[0m\n' "$*"; }
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  if command -v sudo >/dev/null 2>&1; then SUDO="sudo"; fi
fi

# ---- Node.js ----------------------------------------------------------------
if ! command -v node >/dev/null 2>&1; then
  echo "Node.js is not installed. Install Node.js 22 or newer first, for example:" >&2
  echo "  curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash - && sudo apt-get install -y nodejs" >&2
  exit 1
fi
NODE_MAJOR="$(node -p 'process.versions.node.split(".")[0]')"
if [ "$NODE_MAJOR" -lt 22 ]; then
  echo "Node.js $(node -v) is too old; OpenClaw needs 22 or newer." >&2
  exit 1
fi
say "Node.js $(node -v)"

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
    $SUDO npm install -g "openclaw@$VERSION"
  fi
fi
command -v openclaw >/dev/null 2>&1 || { echo "openclaw is not on PATH after install; open a new shell and rerun" >&2; exit 1; }

# ---- exec policy: reads free, writes ask ---------------------------------------
STATE_DIR="${OPENCLAW_STATE_DIR:-$HOME/.openclaw}"
mkdir -p "$STATE_DIR"
POLICY="$STATE_DIR/exec-approvals.json"
if [ -f "$POLICY" ]; then
  say "Keeping the existing exec policy at $POLICY"
elif [ "$ALLOW_WRITES" = "1" ]; then
  say "No allowlist: every command will ask in ClawHQ"
  cat > "$POLICY" <<'EOF'
{
  "version": 1,
  "defaults": { "security": "allowlist", "ask": "on-miss", "askFallback": "deny" },
  "agents": {}
}
EOF
else
  say "Writing a read-only exec policy to $POLICY"
  cat > "$POLICY" <<'EOF'
{
  "version": 1,
  "defaults": { "security": "allowlist", "ask": "on-miss", "askFallback": "deny" },
  "agents": {
    "*": {
      "security": "allowlist",
      "ask": "on-miss",
      "askFallback": "deny",
      "allowlist": [
        { "pattern": "/usr/bin/cat" },
        { "pattern": "/usr/bin/head" },
        { "pattern": "/usr/bin/tail" },
        { "pattern": "/usr/bin/less" },
        { "pattern": "/usr/bin/grep" },
        { "pattern": "/usr/bin/wc" },
        { "pattern": "/usr/bin/ls" },
        { "pattern": "/usr/bin/find" },
        { "pattern": "/usr/bin/stat" },
        { "pattern": "/usr/bin/du" },
        { "pattern": "/usr/bin/df" },
        { "pattern": "/usr/bin/free" },
        { "pattern": "/usr/bin/uptime" },
        { "pattern": "/usr/bin/uname" },
        { "pattern": "/usr/bin/hostname" },
        { "pattern": "/usr/bin/whoami" },
        { "pattern": "/usr/bin/id" },
        { "pattern": "/usr/bin/env" },
        { "pattern": "/usr/bin/ps" },
        { "pattern": "/usr/bin/top" },
        { "pattern": "/usr/bin/pgrep" },
        { "pattern": "/usr/bin/lsof" },
        { "pattern": "/usr/bin/ss" },
        { "pattern": "/usr/bin/netstat" },
        { "pattern": "/usr/bin/dig" },
        { "pattern": "/usr/bin/nslookup" },
        { "pattern": "/usr/bin/curl" },
        { "pattern": "/usr/bin/journalctl" },
        { "pattern": "/usr/bin/systemctl" },
        { "pattern": "/usr/bin/docker" },
        { "pattern": "/usr/bin/git" },
        { "pattern": "/usr/bin/node" },
        { "pattern": "/usr/bin/npm" },
        { "pattern": "/usr/bin/nginx" },
        { "pattern": "/bin/cat" },
        { "pattern": "/bin/ls" },
        { "pattern": "/bin/grep" },
        { "pattern": "/bin/ps" },
        { "pattern": "/bin/df" }
      ]
    }
  }
}
EOF
  echo "   Note: systemctl, docker, git and npm are allowlisted for status and log reads;"
  echo "   the gateway still asks before anything it classes as a mutation, and you can"
  echo "   tighten this file any time."
fi

# ---- pair and install the service -------------------------------------------------
say "Pairing with the gateway as \"$NAME\" and installing the node service"
openclaw connect --service --display-name "$NAME" "$CODE"
say "Done. Approve the pairing in ClawHQ (Settings → Gateways) if it is not approved already."
