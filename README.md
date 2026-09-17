# ClawHQ

A desktop command center for your OpenClaw agent org. Agents on the left grouped into
departments, a chat thread on the right, and settings for each agent and for the gateway
itself. Windows and macOS, built with Wails v3 (Go + React).

```
┌──────────────────────────────────────────┐
│ 🦞 ClawHQ                              │
├────────────┬─────────────────────────────┤
│ EXECUTIVE  │  🎯 Atlas                ⚙  │
│  🎯 Atlas  │  anthropic/claude-opus-5    │
│ MARKETING  ├─────────────────────────────┤
│  📣 Mira   │            what's the plan? │
│ SUPPORT    │  Here's where we are…       │
│  🛟 Kofi   ├─────────────────────────────┤
│            │  [ Message Atlas…   ] [Send]│
└────────────┴─────────────────────────────┘
```

## Installing

Grab a build from [Releases](../../releases): `ClawHQ-darwin-arm64.zip` (Apple Silicon)
or `ClawHQ-windows-amd64.exe` (Windows, portable).

**macOS will refuse to open a browser-downloaded build.** The app is ad-hoc signed but
not notarized, so Safari or Chrome tag it with `com.apple.quarantine` and Gatekeeper
reports it as "damaged". It isn't.

The cleanest route is to download in Terminal, which never sets the quarantine flag:

```bash
curl -L -o ClawHQ.zip https://github.com/surpriseawofemi/clawHQ/releases/latest/download/ClawHQ-darwin-arm64.zip
unzip ClawHQ.zip
mv ClawHQ.app /Applications/
open /Applications/ClawHQ.app
```

If you already downloaded it in a browser, strip the flag instead:

```bash
xattr -dr com.apple.quarantine /Applications/ClawHQ.app
```

Notarizing properly — so it opens with a double-click like any other app — needs a paid
Apple Developer account. Intel Macs and Linux aren't built today; both are a small
change to `.github/workflows/build.yml`.

## Updating

ClawHQ checks GitHub releases every hour. With "Download and install updates
automatically" on (the default, in Settings → Updates and about) a newer release is
downloaded, swapped in and relaunched by itself; off, the panel shows what is
available and waits for Install. Check now behaves the same way as the hourly check.
A machine that builds ClawHQ itself runs the same version as the tag it pushed, so it
never offers itself the update.

The app checks GitHub releases every six hours and can update itself in place —
Settings shows the running version and a Check now button. On macOS the downloaded
artifact is a whole signed `.app` and the updater swaps the bundle wholesale, so the
signature survives and nothing needs re-signing; because the download comes over Go's
HTTP client rather than a browser, Gatekeeper never quarantines it either.

The running version lives in the `VERSION` file, and CI refuses to build a tag that
disagrees with it.

## Building it

Requires Go 1.25+, Node, and the `wails3` CLI
(`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.21`). On Linux the
CLI also needs `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` to compile.

```bash
wails3 dev            # hot-reload dev
wails3 build          # release binary for the host platform
wails3 task build     # see Taskfile.yml for the full task list
```

On first launch ClawHQ pairs itself with your gateway. If the `openclaw` CLI is on
PATH, that's one click; otherwise paste a setup code from `openclaw qr` on the machine
running the gateway.

## Architecture

```
main.go                      app + window, event wiring, auto-connect
services.go                  the three Wails services bound to the frontend
internal/gateway/            the single gateway connection + device pairing
internal/store/              ~/.openclaw/clawhq.json (departments)
internal/supervisor/         openclaw daemon start/stop/restart/status
third_party/openclaw-go/     patched gateway client — see its PATCHES.md
frontend/src/
  api.ts                     adapter over the generated Wails bindings
  state/useFleet.ts          roster, sessions, history, live stream
  components/                Sidebar, ChatView, dialogs, Onboarding
```

Go services are exposed to React through generated bindings; `app.Event.Emit` pushes
gateway events the other way. `frontend/src/api.ts` is the only file that knows about
Wails, so the components stay transport-agnostic.

## How it connects

Authentication is **device-based**. A client that presents only a credential is
authenticated but granted an empty scope array, and every RPC then fails with
`missing scope: operator.read`. Present the same credential *alongside a signed Ed25519
device identity* and the gateway grants scopes and issues a **device token** for later
reconnects. The identity is the thing that matters, not which credential you used:

| Presented | Result |
| --- | --- |
| Shared token alone | connects, `scopes: []` — useless |
| Shared token **+ device identity** | full scopes, device token issued |
| Setup code **+ device identity** | same, via a one-time bootstrap token |

So there are two equally valid ways to add a gateway, and ClawHQ offers both:

- **URL + shared token** — paste `gateway.auth.token` and the address. Used once; the
  device token replaces it, so the shared token is never stored.
- **Setup code** from `openclaw qr`, which carries its own URL.

If the gateway does not auto-approve the device (auto-approval applies to loopback and
any configured `autoApproveCidrs`), the connection parks in a **pending** state showing
ClawHQ's device id. Approve it with `openclaw devices approve <id>` or in the Control
UI under Devices, and ClawHQ retries in the background until it lands — no need to
come back and click anything.

Identities are per gateway, under the OS config dir, so removing one gateway never
disturbs another's pairing.

### Transport is not ClawHQ's problem

ClawHQ only ever sees a URL. An SSH tunnel, a Cloudflare tunnel on a domain, a
tailnet address and a plain LAN bind differ only in what you type:

```
ws://127.0.0.1:18789            # local, or through an SSH tunnel
wss://openclaw.example.com      # cloudflared / reverse proxy
ws://box.tail1234.ts.net:18789  # tailnet
```

Note the gateway's own rule: public endpoints must be `wss://`; plaintext `ws://` is
accepted only for loopback, private RFC 1918 ranges, and `.ts.net`.

### Tool calls in the thread

An assistant message's `toolcall` blocks render as compact rows under its text: a
status mark, the tool name (the `mcp__openclaw__` prefix dropped), and a gist of the
arguments. The matching `tool_result` block, which the gateway stores in a later
user-role message keyed by `tool_use_id`, is paired by id; expanding a row shows the
full arguments and the result text. A call with no recorded result shows as pending.

### Chat

Threads are cached on disk. Every thread this ClawHQ fetches is written to
`~/.openclaw/clawhq-cache.sqlite` (pure-Go SQLite, one row per gateway and session
holding the tail as JSON plus the session's last-updated stamp). Opening a thread
shows the cached copy at once. The gateway is asked only when `sessions.list` says
the session moved past the stamp the copy was fetched at, and then for the recent
window, which is merged by message id on top of the older cached messages. Live
messages from the subscription go into the cache too. After connecting, every
agent's main thread is warmed one at a time, so the first click on any agent finds
its thread ready. Settings → This app shows the cache size and clears it; the
gateway keeps the truth, so clearing costs only the next load.

Three RPCs and one event stream:

| Purpose | Call |
| --- | --- |
| Load a thread | `chat.history { sessionKey, limit }` |
| Subscribe | `sessions.messages.subscribe { key }` — note: `key`, not `sessionKey` |
| Send | `chat.send { sessionKey, message, idempotencyKey }` |
| Receive | `chat` events: `state: status → delta → final`, plus durable `session.message` |

An agent's main thread is the session key `agent:<agentId>:main`.

The Go gateway client needed three fixes to talk to a 2026.9.4 gateway at all —
protocol version, the `bootstrapToken`/`deviceToken` auth slots, and which token the
device signature covers. They live in `third_party/openclaw-go/PATCHES.md` and belong
upstream.

## This machine as a node

ClawHQ can also connect in the **node** role, which points the other way: instead of
reading gateway state, it exposes this computer to agents running on the gateway. That
is what makes "read the code in this folder on my Mac" work when the gateway lives on
another machine — the node dials out, so there are no inbound ports and it behaves the
same over an SSH tunnel, a Cloudflare tunnel, or a tailnet.

It is **on by default** and pairs itself. What it exposes is gated feature by feature:
folders are an explicit list, desktop control is a switch, and commands go through a
policy. Commands advertised:

| Command | Does |
| --- | --- |
| `fs.listDir` | Lists sub-directories, refusing anything outside the shared folders |
| `system.which` | Resolves binaries on PATH |
| `system.run.prepare`, `system.run` | Runs a shell command for the gateway's exec tool, subject to the policy below |
| `system.execApprovals.get`, `.set` | Reports (and accepts) the exec policy in the gateway's own shape |
| `screen.snapshot` | Captures the desktop as a PNG (Windows and macOS) |
| `computer.act` | Mouse, keyboard and scroll in screenshot coordinates (Windows and macOS). Refused unless "Allow desktop control" is on in Settings |
| `system.notify` | Shows a notification on this machine and a banner in ClawHQ — how an agent asks for a human |
| `clawhq.departments.list`, `clawhq.departments.create`, `clawhq.agents.assign` | The org chart, for agents |

### When something breaks

A render error no longer leaves a blank window: the error boundary shows the
message and stack with Reload and Copy details. Every uncaught error, unhandled
promise rejection and boundary catch is appended to `~/.openclaw/clawhq-frontend.log`.
The main window has devtools enabled, so on macOS Safari → Develop → ClawHQ can
inspect it once `defaults write com.clawhq.app WebKitDeveloperExtras -bool true` is set.

### Theme

The sun/moon button in the top bar flips between dark and light on every page and
makes that choice explicit. Settings → App still offers "system" to follow macOS or
Windows again; both controls stay in step.

### Tool allowlist

A gateway running a tool profile (`tools.profile`) only offers agents the tools
in `tools.allow` / `tools.alsoAllow`. ClawHQ adds every plugin tool it registers
to `tools.alsoAllow` when it finds the plugin, after installing it and after an
upgrade, so agents never silently lack `clawhq_issue_create` and friends. It only
ever extends the list.

### Servers

Servers are machines ClawHQ reaches over SSH, with nothing to do with OpenClaw.
Add one under Servers (host, port, user, and the SSH agent, a key file or a
password); the profile stays on this machine in `~/.openclaw/clawhq.json`, and
host keys are trusted on first use in ClawHQ's own `known_hosts` under the app's
config folder, so a changed key is refused until you forget and re-add the server.

The health page runs one script through the user's login shell and reports the
system, uptime, load, disk, memory, git, Node.js, and Claude Code: installed,
version, path, and whether it is logged in (credentials file or API key) and as
whom. "Install Claude Code" runs Anthropic's native installer for that user.
The Terminal tab is a real login shell (xterm in the page, a PTY over SSH in
Go) that opens in the project folder; run `claude` there for the full Claude
Code, and `/login` once to sign in.

### Images in replies

When an agent attaches an image to a reply (the gateway stores it as an image
artifact on the message; the old `MEDIA:<path>` line is the same thing), Chat
shows it inline under the text, click to enlarge. ClawHQ resolves the artifact
with `artifacts.download` over the authenticated WebSocket, downloads the
short-lived ticketed URL on the gateway's HTTP side from Go, and hands the bytes
to the page as a data URL; Go caches them. The `MEDIA:` line itself is hidden.

### Live pages

Issues, Team Chat, Tasks, Activity, Office and Digest refresh on their own while
visible (every 6 to 30 seconds depending on the page), again when the window
comes back to the front, and on the refresh button in the page header. Gateway
events still trigger an immediate refresh when they arrive, but changes made
from the tool-bridge process of a CLI-backed agent raise no event ClawHQ can
hear, so polling is what keeps those pages honest.

### Plugin state file

The plugin keeps its state in `<state dir>/clawhq/state.json`. With CLI-backed
agents the gateway and the tool bridge it spawns both load the plugin, so two
processes write that file. Since plugin 0.2.5 every write is a fresh
read-modify-write under a lock file (`state.json.lock`, stale after ten seconds),
and nothing is cached between calls. Earlier versions cached the state per
process, and a write from one side erased what the other had filed.

### Agents on a CLI backend

Agents whose runtime is `claude-cli` (Claude Code) or Codex run in a separate
process and only get gateway tools through OpenClaw's MCP loopback bridge. The
gateway starts that bridge for a run only when the tool allowlist names a plugin
or `group:plugins`, so ClawHQ adds `group:plugins` to `tools.alsoAllow` alongside
the tool names. Note that the gateway's `tools.effective` RPC does not list
plugin tools even when agents have them; ask an agent.

### Super Boss Chat

Every agent gets one session that belongs to the human: `agent:<id>:superboss`,
labelled "Super Boss Chat". ClawHQ opens it when you click an agent and creates it
the first time (fixed key, so the gateway adopts the existing one afterwards). The
main thread and every other session stay in the picker for watching agent-to-agent
work. Nobody has to tell the agents: the plugin's prompt hook explains the session
to whichever agent is answering in it, and its tool hook redirects any
`sessions_send` aimed at a Super Boss Chat to that agent's main session, refusing
sends by label. Needs plugin 0.2.1.

### Team Chat

Laid out like a chat app: channels and members on the left, the conversation on
the right. Click an agent in the member list to put `@its-id` in the box; typing
`@` offers the list. Members show a presence dot and "typing in #room…" while an
agent is answering. The board and presence come from the gateway plugin (0.2.3).

- **#room**: the shared board. `@agent …` runs a turn for that agent in its own
  `agent:<id>:team` session and the plugin posts the reply back when the run
  ends; `@all` does that for every agent. No mention: the post sits on the board
  and each agent sees the posts it has not been shown at the start of its next
  turn. When an agent's reply mentions another agent, ClawHQ gives that agent a
  turn too; chains stop after three hops so two agents cannot loop. Agents post
  on their own with `clawhq_team_post`, read with `clawhq_team_read`, and an
  agent mentioning `@boss` or `@all` rings the bell.
- **Super boss**: every agent's Super Boss Chat merged into one timeline, so
  everything agents have said to you is in one place, with who said it and when.
  `@agent …` sends into that agent's Super Boss Chat, `@all …` into everyone's.

### Issues

Everything agents need from you, one item at a time, and the tasks you hand out.
The list on the left is ordered by what needs you first (🔴 needs you, 🟡 in
progress, ✅ resolved), then urgency, then age, with the agent's avatar and the
kind (question, task, issue, improvement). Click one to see the ask and the thread;
the answer box sends your reply into the plugin and into that agent's Super Boss
Chat as a turn, so the agent acts on it and closes the item with a note through
`clawhq_issue_update`. "+ Task for an agent" creates a task the same way.

Agents file items with `clawhq_issue_create` (one per ask; the plugin's briefing
tells every agent to do that instead of bundling asks into one message), list
theirs with `clawhq_issues_list`, and a new item rings the bell. A long report an
agent already sent to Super Boss Chat can be split from Team Chat → super boss:
"File as N issues" makes one item per bold heading. Needs plugin 0.2.4.

Chat bubbles in Team Chat and Issues render Markdown, so `**bold**` shows bold.

### Where Chat opens

Chat reopens on the agent and session that were open last time, saved per gateway
on this machine (localStorage). A saved agent that has left the roster, or a saved
session that no longer exists, falls back to the gateway's default agent and its
main thread.

### Enrolling another machine

Settings → Machines → Add a machine mints a one-time setup code on the gateway
(`device.pair.setupCode`), swaps the loopback address the gateway writes into it for
the address the new machine will dial, and shows one command for it. That address is
the field above the button: prefilled from any saved gateway that is not loopback (a
ClawHQ on the gateway host itself connects over `ws://127.0.0.1`, which no other
machine can use), editable, and refused when it is still loopback.

```bash
curl -fsSL https://raw.githubusercontent.com/surpriseawofemi/clawHQ/main/scripts/node.sh | bash -s -- --code <code> --version 2026.9.4
```

`scripts/node.sh` checks for Node.js 24 (OpenClaw 2026.9.4 refuses 22 and 25),
installs the OpenClaw CLI at the gateway's version, writes
`~/.openclaw/exec-approvals.json` for the chosen mode, then runs
`openclaw connect --service`, which pairs and installs the node host as a system
service. Every machine has one of three modes: **auto** (agents run anything there
without asking), **semi-auto** (the default: cat, tail, journalctl, systemctl, docker,
git and other reads run freely, everything else asks you in ClawHQ) and **manual**
(everything asks). `--mode` picks it at enrol time and Settings → Machines changes it
later for any online node through `exec.approvals.node.set`, which is the same policy
file the node host reads; a ClawHQ desktop maps it onto its own off / ask / allow
switch. The pairing request shows up in the same panel for approval, the
machine appears under Machines and in the Office's Machines room, and its commands
land in the shared history through the plugin. The machine must reach the gateway,
so it sits on the same tailnet or the gateway is published with Tailscale Funnel.
ClawHQ itself is not needed on it.

The script never replaces the machine's system Node.js. If the one on PATH is not 24
(or 26.1+), it unpacks an official Node.js 24 build into `/opt/node24`, installs the
OpenClaw CLI under it, and the node service records that absolute path. Apps on an
older system Node keep running untouched. `--node-path <dir>` points it at a Node you
already have.

Machines lists every paired node with a glyph in front of the name: 🦞 for a ClawHQ
desktop, ⚙️ for OpenClaw's own node host. Both can run on one machine (the gateway
box usually does) and each is its own node. Node hosts get the auto / semi-auto /
manual selector; ClawHQ desktops show their Trust setting instead, which is changed
under Settings → Trust on that ClawHQ. ClawHQ desktops report the app version to
the gateway, so the list also shows which release each one runs.


### Pairing is automatic

Two special cases are handled without you. A gateway does not device-pair clients
on its own machine; it wants its shared token, and answers "gateway token missing".
When the node role hits that, ClawHQ reads the token from the local
`~/.openclaw/openclaw.json` (or `OPENCLAW_GATEWAY_TOKEN`) and connects once with it;
the gateway then issues a device token as usual. And when a release changes the
node's command surface, the fresh identity that pairing needs would leave the old
one behind as a ghost desktop, so the operator side removes the old node pairing
from the gateway as part of the re-pair.

A node needs no credential to ask for pairing: it presents its device identity, the
gateway parks it as a pending request, and an operator approves it. ClawHQ is both
sides of that. As soon as the operator connection is up it starts the node role,
watches `node.pair.requested` (and polls `node.pair.list` as a fallback), approves its
own request, and the node's retry loop connects. Nothing to paste, nothing to click.

The gateway records a node's command list at approval time and nothing can rewrite it
in place, so a changed surface needs a fresh pairing. ClawHQ keeps the list it paired
with next to the node identity and drops the pairing when the compiled-in list differs;
after connecting it also compares the gateway's approved list (`node.list`) with its
own and re-pairs once per launch if commands are missing. **Re-pair now** in Settings
forces the same thing.

Both roles reconnect on their own with backoff when the gateway drops them. A dead
device token stops the operator retry and shows the pairing form instead.

### Commands agents run here

The gateway's exec tool with `host=node` calls `system.run.prepare` for a canonical
plan, applies its own approval policy, then calls `system.run`. ClawHQ's policy sits on
top, in Settings → node:

- **Ask me** (default): allowlisted commands run; anything else shows a banner with
  Allow once / Always / Deny. Nobody answering within two minutes means deny.
- **Run without asking**: everything runs.
- **Off**: everything is refused.

The allowlist takes an exact command, a bare program name (`git`), or a prefix ending
in `*` (`npm run *`). When the gateway's own policy already asked an operator and got a
yes, it sends `approved: true` and ClawHQ does not ask again. The mode can be
overridden per agent (Settings → This machine, or "Trust <agent>" on a banner), so a
trusted agent runs without asking while a new one still asks; the agent id comes from
the exec plan the gateway sends. Gateway-side approval
requests (`exec.approval.requested`) show up as the same banners, because ClawHQ holds
the `operator.admin` scope; they are answered with `exec.approval.resolve`.

Output is capped at 256 KB per stream and commands at ten minutes. The generic
`node.invoke` RPC refuses `system.run`, so an agent cannot reach it except through the
exec tool and its approval policy.

Every `system.run`, whether it ran or was refused, is written to
`~/.openclaw/clawhq-exec-log.json` (last 500, output tail of 4 KB each) and shown in
Settings → Command history: the agent, the time, the decision (allowlisted, allowed
once, always, approved on the gateway, run without asking, denied, nobody answered,
off), the exit code, and the output on demand. It is this machine's log only; commands
that run on the gateway host are in the gateway's own logs.

### Seeing and driving a remote desktop

The desktops button opens the viewer window on a list of paired machines with
Connect and Forget; nothing is captured until you connect. A desktop picked in the
sidebar opens straight to it. **Take control** sends your clicks, scroll,
keystrokes and pastes to that machine as `computer.act` actions and pulls a fresh frame
after each one. Cmd on a Mac keyboard is sent as Ctrl to a Windows desktop and as Cmd to
a Mac. The text box at the bottom sends a string verbatim, which is the reliable way to
enter a password. Every action is a gateway round trip, so expect a beat of latency;
this is for logging into an account for an agent, not for using the machine all day.

On macOS capture uses `screencapture` and input uses CGEvent. The first screenshot
asks for Screen Recording and the first input action asks for Accessibility; both are
granted per app in System Settings → Privacy & Security, and Screen Recording usually
needs an app restart. Screenshots are in Retina pixels and are mapped back to display
points, so coordinates land where the screenshot shows them.

A Mac with more than one display reports `screenCount` in every snapshot; the viewer
shows a display picker, sends `screenIndex` with each capture and action, and the
node maps coordinates against that display's own origin (CoreGraphics' active display
list, the same order `screencapture -D` uses). Windows captures the whole virtual
desktop as one image. The clipboard crosses too: `clawhq.clipboard.get` and
`clawhq.clipboard.set` (pbpaste/pbcopy, PowerShell, wl-clipboard or xclip) are node
commands behind the same desktop-control switch, exposed to agents as
`clawhq_clipboard_get`/`_set` and to you as Send clipboard / Fetch clipboard while in
control. Adding the commands changes the node surface, so the first launch after this
release re-pairs the node once, by itself.

When an agent needs you it can call `system.notify` on the node role of the machine you
are sitting at. ClawHQ shows the message as a banner with **Open chat** and **Open
desktop** shortcuts, and posts an OS notification through the platform's own centre
(Wails' notifications service), so it carries ClawHQ's name and icon rather than a
script runner's. macOS asks once for permission.

A notification addressed to one machine is copied to every other ClawHQ on the
gateway. The gateway's `node.event` channel drops event names it does not know, and
a node cannot address other nodes, but an operator can invoke any node, and every
ClawHQ runs both roles. So the operator side of the machine that received the
notification sends `system.notify` to each other connected ClawHQ node with a `relay`
flag and the origin machine's name; a relayed copy is shown and stored but not
forwarded again. Whichever node an agent addresses, you see it where you are.

Every notification is also kept in `~/.openclaw/clawhq-notifications.json` (the last
500) and listed under Settings → Notifications, newest first, with Open chat and
delete. The bell in the sidebar header shows how many are unread; opening the page
marks them read. So a request made while nobody was at the keyboard is still there.

The gateway's own notify action does not say which agent sent it, so ClawHQ names the
sender two ways. Agents get a `clawhq_ask_human` tool (published with the department
tools) whose parameters include their agent id, and its description tells them to
pass it. For plain notify calls, ClawHQ checks which agent has a run in flight at that
moment via `sessions.list`; exactly one running agent means it was that one, otherwise
the banner says "An agent".

### Node invokes are events, not frames

Worth knowing if you extend the command surface: a 2026.9.4 gateway does **not** send
`invoke` frames. It emits a `node.invoke.request` event whose params arrive as a JSON
*string*, and expects the answer as a `node.invoke.result` RPC:

```
event  node.invoke.request { id, nodeId, command, paramsJSON, timeoutMs, idempotencyKey }
rpc    node.invoke.result  { id, nodeId, ok, payloadJSON | error }
```

The result field is `id`, not `invokeId`. Get that wrong and the call simply times out
on the caller with no diagnostic. The Go client's `WithOnInvoke` frame path is dead code
against this gateway version; it is kept only for older ones.

### Changing the command surface

A node's commands are served from its **approved pairing record**, and nothing can
rewrite that in place — `node.pair.request` is refused to a node ("unauthorized role:
node") *and* to an operator ("unknown method"). The way to change the surface is to
re-pair the node, which ClawHQ now does by itself (see "Pairing is automatic").

### Custom commands: the allowlist, not a plugin

ClawHQ's own verbs — `clawhq.departments.list`, `clawhq.departments.create`,
`clawhq.agents.assign` — reach agents as plugin tool descriptors published with
`node.pluginTools.update`. A descriptor is silently dropped unless its backing command
is in the gateway's node command allowlist, and the gateway's per-platform defaults do
not know these names:

```
node command not allowed: "clawhq.departments.list"
is not in the allowlist for platform "windows"
```

The allowlist is config, though: `gateway.nodes.commands.allow` (older gateways spell
it `gateway.nodes.allowCommands`). ClawHQ holds `operator.admin`, so on every connect
it reads `config.get`, adds any of its advertised commands that are missing with
`config.patch`, and re-pairs if the approved surface was recorded before that. Both
spellings are tried. After that the descriptors register and agents get
`clawhq_departments_list`, `clawhq_department_create` and `clawhq_agent_assign`.

The org chart stays in this machine's `clawhq.json`, so agents can only reach it while
ClawHQ is running here. A gateway-side plugin would lift that, but it has to be
installed on the gateway host's filesystem, which ClawHQ cannot do over the wire.

### Pending approvals

Devices and nodes asking to pair show up in Settings with their requested roles, scopes
and commands, and can be approved or rejected there. This needs the `operator.pairing`
scope, which ClawHQ requests. Two separate queues back it — `device.pair.list` and
`node.pair.list`, each with its own approve/reject RPC — presented as one list.

That removes the last reason to keep the Control UI around: capability changes on a node
require a fresh approval, so without this you would be back in the terminal every time
the command surface changed.

## Settings

Settings is a page, not a dialog: it takes over the main pane the way a desktop view
does, with a section nav on the left. Connection and This machine cover pairing and
the node role; Departments is ClawHQ's own org chart; the Gateway group is the
gateway's own config, edited over the wire.

Channels (Telegram, Discord, Slack and the rest) are listed from the `channels`
object of the schema, so a channel the gateway learns about tomorrow appears without
a release. Set-up channels come first with their live state from `channels.status`
(refreshed on every `health` event), an enable switch that patches
`channels.<id>.enabled`, and Log out (`channels.logout`); the rest sit behind a
"more channels" toggle. Each one opens the same schema-driven form as a plugin, and
Channel defaults edits `channels.defaults`.

### Config is rendered from the gateway's schema

The gateway publishes a JSON schema for its whole config (`config.schema`, with UI
hints), and its Control UI is generated from it. ClawHQ does the same. Reads go
through `config.get`, writes through `config.patch` (a JSON merge patch checked
against the hash from the last read, so a concurrent edit fails loudly). The
`SchemaForm` component renders any object node: switches, text, numbers, enums,
string lists, string maps, nested groups, and a JSON box for anything else. Secrets
arrive redacted and are only sent back when you type a new value.

- **Plugins** lists `plugins.list`: on, off, and available. A switch calls
  `plugins.setEnabled`; Install sends the row's own `install` descriptor to
  `plugins.install`, which pulls the package onto the gateway host. Each plugin's
  settings are the schema at `plugins.entries.<id>.config`. ClawHub search uses
  `plugins.search`.
- **MCP servers** edits `mcp.servers`. Adding one writes the transport plus a command
  or URL; everything else is the schema form. Removing writes `null`, which merge
  patch treats as delete.
- **Automations** lists `cron.list` and offers pause, resume and run now through
  `cron.update` and `cron.run`.
- **Service** keeps the local start/stop/restart and adds a remote restart through
  `gateway.restart.request`, which works over a tunnel.

## Documents, activity and charter

Each agent's pane has four tabs. **Chat** is the thread. **Activity** shows the
agent's threads by recency and the files it changed most recently, from session
timestamps and workspace modification times. **Documents** lists the
markdown in the agent's workspace through `agents.workspace.list` and renders a file
with `agents.workspace.get`; the charter files are hidden there, folders such as
`memory/` open in place, and rendering is read-only because the gateway accepts
writes only to the charter names. **Charter** edits AGENTS, SOUL, IDENTITY, USER and
MEMORY through `agents.files.get` and `agents.files.set`, with a preview and Cmd-S;
a save applies at the agent's next session start, and saving IDENTITY refreshes the
sidebar.

The Documents tab also holds the **daily update** control. Picking an hour creates a
cron job on the gateway (`cron.add`, named `clawhq-daily-update-<agent>`, an isolated
agent turn) that asks the agent to rewrite `DAILY-UPDATE.md` under fixed headings and
append it to `updates/YYYY-MM-DD.md`, doing no other work. The file then sorts to the
top of Documents. Off removes the job; "Write one now" runs it immediately.

## Departments

OpenClaw has no department concept, so ClawHQ owns that data and keeps it in
`~/.openclaw/clawhq.json`. **Your `openclaw.json` is never written by this feature** —
a custom key inside OpenClaw's own config risks being stripped by `openclaw configure`
or schema validation. On first run, agents are filed into departments whose name their
id already hints at; everything after that is yours to arrange.

Agent settings *do* write real OpenClaw config, via `agents.update`: identity (name,
emoji, theme), model, thinking level, and `subagents.allowAgents` — the reporting line
that decides who each agent may delegate to.

### With the ClawHQ gateway plugin

`plugin/` holds `clawhq-openclaw-plugin`, an OpenClaw gateway plugin. When it is
installed on the gateway, ClawHQ notices on connect (`clawhq.version`) and switches:

- The org chart lives on the gateway. A gateway with none yet takes this machine's
  chart once (`clawhq.org.import`); after that `clawhq.json` is a mirror, every edit
  is written through (`clawhq.org.*`), and a `clawhq.org.changed` event refreshes the
  mirror everywhere. Every ClawHQ sees one chart.
- Agents get `clawhq_departments_list`, `clawhq_department_create`,
  `clawhq_agent_assign` and `clawhq_ask_human` from the plugin, with their real agent
  id supplied by the runtime; the node stops publishing its own copies of those four
  tools. Agents are told their department in the system prompt.
- `clawhq_ask_human` writes to the gateway's inbox and broadcasts `clawhq.notice`;
  every connected ClawHQ shows it, so the node-to-node relay is not needed. On
  connect, notices raised while this ClawHQ was away are fetched (`clawhq.inbox.list`)
  into the local history, already marked read.
- Command history is pushed to the gateway too (`clawhq.exec.append`, tagged with the
  machine name), so one place holds what every machine ran.

Without the plugin nothing changes. Settings → Plugins shows whether it is installed
and installs it from ClawHub. After every connect ClawHQ also asks ClawHub (through
`plugins.search`) for the newest version; with "Download and install updates
automatically" on, a newer plugin is installed straight away, otherwise the page
offers an Update button. The gateway has no in-place plugin update, so an update is
uninstall, install with capability consent, enable, with a wait for the gateway's own
restart after each of the first two; the status line shows the step. The app itself
checks for its own update fifteen seconds after launch and hourly after that. The plugin needs
`plugins.entries.clawhq.hooks.allowConversationAccess` and `allowPromptInjection` set
to true for its hooks; ClawHQ patches those when it installs the plugin.

## How a page is built

Every page uses one frame, `frontend/src/components/layout/Shell.tsx`. The frame is a
top bar across the window (brand, "‹ Menu", the page name, then the global actions:
gateway pill, search, notifications, desktops, settings), and under it either a side
panel plus content or content alone. A page renders `<Shell shell={…} title="…"
side={…}>` and fills the content, starting with `<ContentHead>` (title, subtitle,
controls on the right) and a scrolling `.content-body`. Chat's side panel is the agent
list, Settings' is its section nav, the main menu's is the menu; Activity has none.
Two-column and one-column pages therefore share every pixel of chrome, and a new page
(Office, next) is a component that picks a side panel and fills the content.

## Office, Tasks and Digest

**Office** is the floor plan: a room per department (the org chart), a desk per agent,
and the truth about each desk. Idle, working (with what the agent is doing right now:
its current tool, or "thinking"), needs you (an unread ask or a pending command),
failed (the last run ended badly). A dashed line runs from a parent desk to a child
it spawned while the child works. Live state is the plugin's presence feed
(`clawhq.presence.get`, pushed as `clawhq.presence`), built from the
`agent_turn_prepare`, `before_tool_call`, `after_tool_call`, `subagent_spawned`,
`subagent_ended` and `agent_end` hooks; without the plugin the session list's
run-in-flight flag stands in. A desk opens the agent's card: context use of its main
thread against the model's window, when its memory files last changed, the last
thing it said, open tasks, today's runs, a move between rooms, and Open chat.

**Tasks** is a board the plugin keeps (`clawhq.tasks.*`), so every ClawHQ and every
agent sees the same cards. Run creates a fresh thread for the assigned agent, sends
the task with instructions to report back through `clawhq_task_update`, and moves
the card to Doing; when that thread's run ends, the plugin closes the card with the
agent's last words if the agent did not. Agents get `clawhq_tasks_list`,
`clawhq_task_create` (hand work to another agent) and `clawhq_task_update`.

**Digest** rolls one day up: per agent, every run (from the plugin), tasks, asks,
commands and cost (`sessions.usage` for that day), plus every automation run from
`cron.runs`. Copy as Markdown puts it on the clipboard for a channel or a note.
Automations in Settings gained a Runs button per job with the same run log.

## The main menu and its pages

ClawHQ opens on a main menu: a left column with Chat, Activity, Office (coming) and
Settings, and a glance at the org on the right (what needs you, who is working,
agents, departments). Each entry is its own full page with a "‹ Menu" button at the
top left to come back. Chat is the agent view, with the agent sidebar; Activity is
one timeline for every agent, so you can see what they did and whether anything
needs you without opening them one by one. Rows on Activity come from three places: agent runs recorded by the gateway plugin (`clawhq.activity.list`,
refreshed on the `clawhq.activity` event, with the last thing the agent said), the
notification history, and the command history. Without the plugin, thread updates
from `sessions.list` stand in for runs. "Needs you" collects unread notices and
failed runs; "Working now" lists sessions with a run in flight. Filters by
department and agent, and a row opens that agent's thread.

The in-app notification banner leaves after five seconds; the history page keeps it.
Tool calls in a thread are hidden unless "Show tool calls in chat" is on in
Settings → This app.

## Search and appearance

⌘K (Ctrl-K elsewhere), or the magnifier in the sidebar, opens a palette that finds
agents by name, threads by label, and lines said in threads this window has already
loaded; Enter opens the hit. The gateway has no message search, so text matches
cover fetched threads only, and unopened threads match by title. Appearance
(Settings → This app) follows the system by default or forces dark or light; the
choice is stamped on `<html data-theme>` and kept in localStorage, so it is per
machine.

## Usage

Settings → Usage shows tokens and cost per agent, added up by department, for the
last 7, 30 or 90 days. A multi-agent gateway refuses an unscoped `sessions.usage`, so
the page sends one call per agent with `agentId`, `startDate` and `endDate` (the two
dates must travel together) and sums the `totals`. Cost is whatever the gateway can
price; calls it cannot price, such as a CLI-backed model, are counted as "without a
price" instead of showing as free. Opening an agent lists its five heaviest sessions.

## Health and logs

Settings → Health and logs reads the gateway's `health` snapshot (event loop load,
config hot reload, heartbeat, plugins, channels, one row per agent) and refreshes it
whenever the gateway pushes a `health` event. Below it the gateway's own log file is
tailed with `logs.tail`: the first call takes the last 300 lines, later calls pass the
returned cursor so only new lines cross the wire, every three seconds while Follow is
on. Lines are parsed when they are JSON (time, level, subsystem, message) and shown raw
otherwise; the filter box and the level picker work on what is already loaded, up to
2000 lines.

## Gateway control

The Settings pane can start, stop and restart the gateway. It shells out to
`openclaw daemon`, which already drives the right service manager per OS (schtasks on
Windows, launchd on macOS, systemd on Linux). ClawHQ deliberately does not spawn its
own gateway process — that would fight the installed service for the port and the
gateway lock file.

## Menu bar and login

ClawHQ keeps an icon in the menu bar (system tray elsewhere) with the node's state,
Open ClawHQ, the desktop viewer, Launch at login, Keep running when the window closes,
Check for updates and Quit. With "keep running" on (the default, in Settings → This
app), closing the window hides it instead of quitting, so the node role,
notifications and approvals keep working; the icon or the Dock brings the window
back. Launch at login writes `~/Library/LaunchAgents/com.clawhq.app.plist`, which runs
`open -a` on the bundle so it survives an update that replaces the app; on Windows it
is the `HKCU\...\Run` key. Both are plain files or keys the user can remove by hand.

## Known gaps

- **Wails v3 is beta** (`v3.0.0-beta.21`). The desktop API is stable enough to ship on,
  but expect churn until GA.
- **One client id.** The protocol has a closed enum of client ids and ClawHQ connects
  as `gateway-client`. Two clients sharing that id fight over the device token, so don't
  run ClawHQ and another `gateway-client` app against one gateway.
- **The pending-approval path is untested.** Loopback always auto-approves, so the
  waiting-for-approval flow has only been exercised against the docs, not a real remote
  gateway that defers approval.
- **Gateway start and stop are local-only.** They shell out to the `openclaw` CLI on
  the machine ClawHQ is running on. Restart works remotely; start and stop cannot,
  since a stopped gateway has no RPC to answer.
- **ClawHub installs use an unverified source name.** Official plugins install with
  the descriptor the gateway hands out. Search results are sent as
  `{source: "clawhub", packageName}`; if the gateway spells that differently its
  validation error is shown verbatim.
- **One connection at a time.** Several gateways can be saved and switched between, but
  only the active one is connected; the sidebar shows its agents alone.
- **Departments live on one machine.** Agents manage them through this machine's node
  role, so they are out of reach while ClawHQ is closed here. Moving them to a gateway
  plugin needs files on the gateway host.
- **Exec allowlist entries are ClawHQ's own shape.** The gateway's Control UI pushes
  binary-path patterns through `system.execApprovals.set`; only its security level is
  mapped onto the mode here, the patterns are not imported.
- **macOS input assumes a US keyboard layout** for single-character keys, and
  `screenGeometry` reports the main display only, so secondary displays do not map.
- **Linux has no capture or input yet.**
- **Uptime** shows "unknown" when `openclaw daemon status` omits it.
- Attachments, tool-call rendering, and multi-session-per-agent views are not built yet;
  only each agent's main thread is shown.
