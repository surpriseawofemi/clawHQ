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
#
# What it does: checks Node.js 24, installs the OpenClaw CLI, writes the exec policy
# for the chosen mode, then
# pairs with the gateway and installs the node host as a system service.
set -euo pipefail

CODE=""
VERSION="2026.9.4"
NAME="$(hostname -s 2>/dev/null || hostname)"
MODE="semi"
while [ $# -gt 0 ]; do
  case "$1" in
    --code) CODE="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --mode) MODE="$2"; shift 2 ;;
    --allow-writes) MODE="manual"; shift ;;
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
if ! command -v node >/dev/null 2>&1; then
  echo "Node.js is not installed. OpenClaw $VERSION needs Node.js 24 (not 25). Install it first, for example:" >&2
  echo "  curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash - && sudo apt-get install -y nodejs" >&2
  exit 1
fi
NODE_MAJOR="$(node -p 'process.versions.node.split(".")[0]')"
if [ "$NODE_MAJOR" -lt 24 ] || [ "$NODE_MAJOR" -eq 25 ]; then
  echo "Node.js $(node -v) will not do; OpenClaw $VERSION needs Node.js 24 (or 26.1+). On Ubuntu:" >&2
  echo "  curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash - && sudo apt-get install -y nodejs" >&2
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
