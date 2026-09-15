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

### Pairing is automatic

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

Pick a desktop in the sidebar to watch it. **Take control** sends your clicks, scroll,
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
