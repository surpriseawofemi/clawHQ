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

Grab a build from [Releases](../../releases): `ClawHQ-macos-arm64.zip` (Apple Silicon)
or `ClawHQ.exe` (Windows, portable).

**macOS will refuse to open it the first time.** The app is ad-hoc signed but not
notarized, so macOS quarantines anything downloaded and reports it as "damaged". It
isn't — clear the flag once:

```bash
xattr -dr com.apple.quarantine /Applications/ClawHQ.app
```

Notarizing properly requires a paid Apple Developer account. Intel Macs and Linux
aren't built today; both are a small change to `.github/workflows/build.yml` if you
want them.

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

It is **off by default** and scoped to an explicit folder list. Commands advertised:

| Command | Does |
| --- | --- |
| `fs.listDir` | Lists sub-directories, refusing anything outside the shared folders |
| `system.which` | Resolves binaries on PATH |
| `screen.snapshot` | Captures the desktop as a PNG (Windows only so far) |

Turning it on needs the gateway token once (a node gets its own device identity, so it
appears separately from the operator in `openclaw devices list`) and an operator has to
approve the pairing — ClawHQ can do that itself, see below.

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
**re-pair the node**: `NodeService.RePair` drops its device identity so the next enable
raises a fresh approval showing the new commands.

### Custom commands need tool descriptors, not invoke names

ClawHQ's own verbs — `clawhq.departments.create`, `clawhq.agents.assign` — are
implemented in `internal/node` but are **not reachable yet**. The gateway enforces a
per-platform command allowlist:

```
node command not allowed: "clawhq.departments.list"
is not in the allowlist for platform "windows"
```

Publishing them as plugin tool descriptors via `node.pluginTools.update` does not rescue
them either. A descriptor is accepted and then **silently dropped** — the call returns
`ok` with `tools: []` and the node's `nodePluginTools` stays empty.

The reason, established by experiment: a descriptor's backing `command` must already be
in the allowlist. An otherwise identical descriptor backed by `fs.listDir` registered
fine; the `clawhq.*` ones did not. `gateway.nodes.pluginTools.enabled` defaults to
true, so the switch is not the problem.

**A node cannot introduce new verbs on its own.** Reaching agents with ClawHQ's own
commands needs either a gateway-side plugin that registers them, or routing through a
command that is already allowlisted.

### Pending approvals

Devices and nodes asking to pair show up in Settings with their requested roles, scopes
and commands, and can be approved or rejected there. This needs the `operator.pairing`
scope, which ClawHQ requests. Two separate queues back it — `device.pair.list` and
`node.pair.list`, each with its own approve/reject RPC — presented as one list.

That removes the last reason to keep the Control UI around: capability changes on a node
require a fresh approval, so without this you would be back in the terminal every time
the command surface changed.

## Departments

OpenClaw has no department concept, so ClawHQ owns that data and keeps it in
`~/.openclaw/clawhq.json`. **Your `openclaw.json` is never written by this feature** —
a custom key inside OpenClaw's own config risks being stripped by `openclaw configure`
or schema validation. On first run, agents are filed into departments whose name their
id already hints at; everything after that is yours to arrange.

Agent settings *do* write real OpenClaw config, via `agents.update`: identity (name,
emoji, theme), model, thinking level, and `subagents.allowAgents` — the reporting line
that decides who each agent may delegate to.

## Gateway control

The Settings pane can start, stop and restart the gateway. It shells out to
`openclaw daemon`, which already drives the right service manager per OS (schtasks on
Windows, launchd on macOS, systemd on Linux). ClawHQ deliberately does not spawn its
own gateway process — that would fight the installed service for the port and the
gateway lock file.

## Known gaps

- **Wails v3 is beta** (`v3.0.0-beta.21`). The desktop API is stable enough to ship on,
  but expect churn until GA.
- **One client id.** The protocol has a closed enum of client ids and ClawHQ connects
  as `gateway-client`. Two clients sharing that id fight over the device token, so don't
  run ClawHQ and another `gateway-client` app against one gateway.
- **The pending-approval path is untested.** Loopback always auto-approves, so the
  waiting-for-approval flow has only been exercised against the docs, not a real remote
  gateway that defers approval.
- **Gateway service controls are local-only.** Start/Stop/Restart shell out to the
  `openclaw` CLI on the machine ClawHQ is running on, so they do nothing for a gateway
  reached over a tunnel. `gateway.restart.request` would make restart work remotely;
  start/stop cannot, since a stopped gateway has no RPC to answer.
- **One connection at a time.** Several gateways can be saved and switched between, but
  only the active one is connected; the sidebar shows its agents alone.
- **Agents cannot manage departments yet.** The handlers and tool descriptors exist, but
  the gateway drops descriptors whose backing command is not allowlisted, so they never
  reach an agent (see above).
- **The node exposes no shell.** `system.run` is deliberately not implemented — the
  gateway reserves it for the exec tool and its approval policy, and it deserves its own
  design pass rather than being bolted on. Desktop control (`computer.act`,
  `screen.snapshot`) is not implemented either.
- **Uptime** shows "unknown" when `openclaw daemon status` omits it.
- **macOS is untested** — the code is cross-platform and the build targets it, but it
  has only been run on Windows so far.
- Attachments, tool-call rendering, and multi-session-per-agent views are not built yet;
  only each agent's main thread is shown.
