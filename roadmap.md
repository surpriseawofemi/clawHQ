# ClawHQ roadmap

Sizes: S under a day, M a few days, L a week or more. Order changes as the gateway
changes. Shipped items keep their number so discussions stay anchored.

## Principles that decide the order

- **Automatic by default, gated by feature.** Connecting should never need a step.
  What is exposed once connected is a set of explicit switches.
- **The gateway is the server in the middle.** No second service, no database of our
  own. State that must outlive one machine goes to the gateway.
- **Generate from the schema.** Plugin, MCP and channel settings are read from the
  gateway's own schema, so the app keeps up with the gateway without a release.
- **Nothing runs silently.** Commands on this machine are logged and, unless
  allowlisted, asked about. Denied is the default when nobody answers.

## Shipped

| # | Item | Release |
| --- | --- | --- |
| A | Node pairs itself: no token, self-approval, reconnects with backoff | v0.1.3 |
| B | Departments reach agents: gateway allowlist patched over the wire, node tools registered | v0.1.3 |
| C | Agents can run commands: `system.run` behind ask / allow / off plus an allowlist, one banner for node-side and gateway-side approvals | v0.1.3 |
| D | macOS desktop control: capture and input, Retina-aware | v0.1.3 |
| E | Own icon and header on macOS | v0.1.3 |
| 1 | Settings as a page with a section nav | v0.1.3 |
| 2 | Schema-driven gateway config (`config.schema`, `config.patch`) | v0.1.3 |
| 3 | Plugins and MCP servers pages, one-click install of official plugins, ClawHub search | v0.1.3 |
| 4 | Connection state in the sidebar, gateway name in the pill | v0.1.3 |
| 9 | Automations: list, pause, resume, run now | v0.1.3 |
| 13 | Remote gateway restart | v0.1.3 |
| – | Gateway picker at launch, auto-connect switch, gateway switcher and rename | v0.1.3 |
| – | Desktops in their own native window, pin on top | v0.1.3 |
| – | Chat opens with the recent tail, loads the rest on request | v0.1.3 |
| – | Notifications from ClawHQ itself, naming the agent; `clawhq_ask_human` tool | v0.1.5 |
| – | Hourly update checks with an auto-install switch | v0.1.6 |
| – | Notification history page and unread bell | v0.1.7 |
| 18 | Documents and Charter tabs per agent | v0.1.8 |
| – | Stable local signing identity so macOS permission grants survive rebuilds | main |
| 5 | Tool calls in the chat thread: each `toolcall` block under the agent's message with its paired `tool_result`, collapsed to name plus a gist, expandable to arguments and output | v0.1.12 |
| 19 | Daily updates (a per-agent cron job that writes `DAILY-UPDATE.md`, set from the Documents tab) and an Activity tab (threads by recency, recently changed files) | v0.1.11 |
| 20 | Notifications reach every ClawHQ: the receiving machine's operator side fans out `system.notify` to every other connected ClawHQ node (the gateway drops unknown `node.event` names, so that route was out) | v0.1.10 |
| 6 | Command history: every `system.run` on this machine (agent, time, decision, exit code, output tail) in `clawhq-exec-log.json`, shown under Settings → Trust | v0.1.13 |
| 7 | Health and logs page: the `health` snapshot (refreshed on the `health` event) and a cursor-following `logs.tail` with filter and level picker | v0.1.14 |
| 8 | Per-agent command policy: an override per agent id (trusted, ask, off) beside the machine-wide mode; "Trust <agent>" on a banner | v0.1.15 |
| 10 | Channels page: catalogue from the schema, live state from `channels.status`, enable/disable, log out, schema-driven settings and channel defaults | v0.1.16 |
| 15 | Usage per agent and department: one `sessions.usage` call per agent for a 7/30/90-day window, totals rolled up along the org chart, unpriced calls counted rather than shown as free | v0.1.17 |
| 12 | Menu bar presence and launch at login: tray icon with node state and a menu, closing the window hides it while "keep running" is on, launch agent (macOS) or Run key (Windows) | v0.1.18 |
| 16 | ⌘K search palette (agents, thread labels, loaded message text) and an appearance switch (system, dark, light) | v0.1.19 |
| 14a | Secondary displays (display picker, per-display coordinate mapping on macOS) and clipboard sync (`clawhq.clipboard.get/set`, agent tools, Send/Fetch clipboard while in control) | v0.1.20 |
| 24 | Thread cache: SQLite on each machine, instant open from disk, gateway asked only when the session stamp moved, main threads warmed after connect | v0.1.22 |
| 25 | Activity page: one timeline for every agent (plugin runs, notices, commands), Needs you and Working now, department/agent filters; notice banner auto-dismisses; tool calls in chat behind a setting, off by default | v0.1.25 |
| 26 | Main menu shell: the app opens on a menu (Chat, Activity, Office soon, Settings) with an org glance; every page is full-window with a ‹ Menu button | v0.1.26 |
| 27 | One page frame for everything (`layout/Shell.tsx`): global top bar with back, page name, gateway pill, search, bell, desktops, settings; side panel + content or content alone; shared ContentHead; Chat, Settings, Activity and the menu all use it | v0.1.28 |
| 28 | Office (floor plan with live presence, delegation lines, agent card with context and memory health), Tasks board (plugin-kept, agent tools, run in a fresh thread, closes with the run), Daily digest (runs, tasks, asks, commands, cron runs, cost; Markdown copy), cron run history in Automations; plugin 0.2.0 | v0.1.30 |
| 22 | Enrol any Linux machine as a node: Settings → Machines mints a one-time code and one paste-able command; `scripts/node.sh` installs the OpenClaw CLI, a read-only exec policy and the node service; Machines room in Office | v0.1.32 |
| 22b | Join command carries a reachable gateway address: editable field, prefilled from a non-loopback saved gateway, loopback refused (a ClawHQ on the gateway host minted `ws://127.0.0.1` codes) | v0.1.34 |
| 22c | Machines: host glyph (ClawHQ desktop vs OpenClaw node host), Trust label for desktops, app version in the node hello, valid exec-approvals snapshot (path/exists/hash/file), enrol script installs Node 24 side by side in `/opt/node24` | v0.1.35 |
| 29 | Chat reopens on the last open agent and session, per gateway; one themed dropdown style for every `select` (custom chevron, no browser default box) | v0.1.36 |
| 30 | Sun/moon toggle in the top bar flips dark/light on every page; Settings → App picker stays in sync | v0.1.37 |
| 31 | Settings pages use the full width (760px cap removed); checkboxes no longer inherit the text-input box, hints indent under their checkbox | v0.1.38 |
| 32 | Desktops button shows a small online dot instead of a count that read as unread notifications | v0.1.39 |
| 33 | Chat box starts at one line and grows with the draft to five lines, then scrolls | v0.1.40 |
| 34 | No more blank window: an error boundary shows the message and stack with Reload/Copy, uncaught errors and rejections go to `~/.openclaw/clawhq-frontend.log`, devtools enabled on the main window; a saved chat session is restored only once the session list confirms it exists | v0.1.41 |
| 35 | Super Boss Chat: per-agent session reserved for the human, opened by default and created by ClawHQ; plugin 0.2.1 redirects agent sends away from it and briefs agents in-session | v0.1.43 |
| 36 | Team Chat: shared board with @agent / @all / no-mention rules, per-agent team sessions, replies posted by the plugin (0.2.2), unread posts as prompt context, @boss rings the bell | v0.1.44 |
| 37 | Team Chat becomes a chat app: channel + member sidebar with presence and typing, click-to-mention, agents answer each other (hop cap 3, plugin 0.2.3 `team.turn`), and a Super Boss channel merging every agent's Super Boss Chat with @agent/@all sending | v0.1.45 |
| 38 | Issues page: one-by-one items from agents (question/task/issue/improvement, urgency, 🔴🟡✅), answer box that turns into a Super Boss Chat turn, tasks for agents, "File as N issues" splitter for long reports, agent tools + briefing (plugin 0.2.4); Markdown in chat bubbles | v0.1.46 |
| 39 | Plugin auto-update no longer races between ClawHQs: stagger, check `plugins.list` first, skip when another ClawHQ already installed the version; hook policy patched right after install; a deferred gateway restart is tolerated | v0.1.47 |
| 40 | Gateways with a tool profile get every ClawHQ plugin tool added to `tools.alsoAllow` on detect/install/upgrade (agents had only 3 of 12) | v0.1.48 |
| 41 | Activity: "Hide automations & empty runs" filter (cron sessions/runs and successful turns with no summary), on by default, remembered per machine | v0.1.49 |
| 42 | Chat renders agent messages as Markdown (bold, headings, lists, code, tables), streaming included; the raw `**` and `#` are gone | v0.1.50 |
| 43 | Claude CLI agents get the plugin tools: ClawHQ enables `cliBackends.claude-cli.bundleMcp` (the MCP loopback bridge) when any agent runs on that backend; without it no gateway tool reached them, whatever the allowlist said | v0.1.51 |
| 44 | Correction: the bridge key from the docs does not exist in 2026.9.4; what CLI-backed agents need is `group:plugins` in `tools.alsoAllow`, which ClawHQ now adds. Verified live: the main agent lists all 14 clawhq tools | v0.1.52 |
| 45 | Plugin 0.2.5: state store reads fresh and writes under a cross-process lock; the gateway process and the CLI tool-bridge process each load the plugin and the cached copy in one erased issues/tasks/posts written by the other (CEO's diagnosis, confirmed by 3-process test: 120/120 kept) | plugin 0.2.5 |
| 46 | Live pages: Issues, Team Chat, Tasks, Activity, Office and Digest poll while visible (6–30s), refresh when the window comes to the front, and have a refresh button in the header; plugin events raised in the tool-bridge process never reached ClawHQ | v0.1.53 |
| 47 | Chat sidebar: sort by last message (default), name or department order; department grouping can be switched off for one flat list; both remembered per machine; a relative time on each agent | v0.1.54 |
| 48 | Chat queue: a message sent while the agent is still replying waits above the composer (Next, #2…) and goes out when the run ends; Send now stops the run and sends it first; ✕ drops it. The Send button reads Queue while a reply streams | v0.1.55 |
| 49 | Images agents attach to replies show inline in Chat (click to enlarge): the message's image artifact is resolved with `artifacts.download` over the WebSocket, the ticketed HTTP URL fetched by Go and handed over as a data URL, cached; legacy `MEDIA:` lines are hidden | v0.1.56 |
| 50 | Servers page (no OpenClaw involved): SSH profiles kept locally (agent, key file or password; host keys trusted on first use in ClawHQ's own known_hosts), a health page per server (system, disk, memory, git, Node, Claude Code installed/version/login), one-click Claude Code install (Anthropic's native installer), and an xterm terminal over SSH that lands in the project folder. Next: a chat page backed by headless Claude Code, more coding agents | v0.1.57 |
| 51 | Top bar is a real drag region: the stylesheet used Electron's `-webkit-app-region`, which the Wails runtime ignores; `--wails-draggable` added everywhere, interactive controls excluded | v0.1.58 |
| 52 | Smoother live resize: the macOS window is made opaque with a solid backing colour (Wails creates every window transparent), and long lists use `content-visibility: auto` so off-screen rows skip layout during a resize | v0.1.59 |
| 53 | Servers → Files: SFTP browser with breadcrumbs, path box and filter; text editor with save (⌘S), Markdown and image preview; upload from the native picker with progress, download to a save dialog, rename, delete, new folder and file; one SFTP connection per server kept open five idle minutes | v0.1.60 |
| 54 | Servers → Chat: headless Claude Code over SSH (`claude -p --output-format stream-json --include-partial-messages --resume`), streaming text, folded tool calls and results, session picker read from Claude Code's own project folder, history from the session file (shared with the terminal), permission modes auto/semi/manual mapped to `--permission-mode` and an allowlist, a queue for messages typed mid-run, Stop, cost per run, and a bell notification when a run ends | v0.1.61 |
| 55 | Servers → Actions: saved commands per server (templates for pull, disk, logs, services, docker, pm2, nginx restart), ask-before-running flag, one-off command box, output streamed into the page with exit status and Stop; Terminal tab holds several terminals per server kept alive in a registry outside React so they survive page switches, with reconnect | v0.1.62 |
| 56 | Servers polish: compact terminal tab strip, stay on Health after connecting, permission modes named Auto / Semi / Manual with Auto the default | v0.1.63 |
| 57 | Servers: projects per server (picker in the header; each with its own folder, agent, permission mode and sessions; terminals, actions, files and chat follow the active one), a health monitor every 10 minutes with bell warnings (two missed checks, disk ≥ 90%, load above twice the cores; switch per server), and other coding agents: Codex, Gemini CLI and Grok CLI detected on the health page with install and login hints, selectable per project; Codex chat via `codex exec --json`, Gemini and Grok as plain-text chat, marked experimental | v0.1.64 |
| 58 | Servers everywhere: Team Chat lists servers as members (`@name` gives the machine's coding agent a turn and its reply lands on the board; agents can address servers too, hop cap applies), Issues get "Send to server…" with the report coming back as a reply, the Digest gains a Servers section (runs from ClawHQ with cost, plus token counts read from Claude Code's session files so terminal work counts), and OpenClaw agents can hand tasks to a server through plugin 0.2.6 tools (`clawhq_servers_list`, `clawhq_server_task`, `clawhq_server_tasks_list`): the ClawHQ that owns the server claims the task, runs it, and posts the result back | v0.1.65 |
| 59 | Health: coding-agent rows are single-line like the system rows, with Install / Log in / Chat as small buttons at the right | v0.1.66 |
| 60 | Projects as workspaces: projects listed under their server in the sidebar (click to switch, edit, remove, add), each keeping its own file browser position, open file and terminals; terminals belong to a project and open in its folder; a project card on Health reads the folder once (git branch and remote, package.json, go.mod, composer, python, docker, pm2, CLAUDE.md, .env) and suggests actions from it; actions can be per project (shown first) or server-wide | v0.1.67 |
| 61 | Terminals survive a ClawHQ restart: each tab is a tmux session on the server (`tmux -u new-session -A`, mouse on, 20k history), the tab list is saved per project and reattaches on reopen; × ends the session, ⇣ detaches and keeps it running; tmux detected on Health with an Install button; per-server switch, plain shells otherwise | v0.1.68 |
| 62 | tmux status bar hidden in ClawHQ terminals | v0.1.69 |
| 63 | "Continue without a gateway" on the gateway chooser opens ClawHQ on Servers; SSH features need no gateway | v0.1.70 |
| 64 | Servers page keeps its state across page switches: selected server, tab, health results and project cards stay; health is not re-run on return | v0.1.71 |
| 65 | Terminal clipboard: selecting text copies it, right-click (or the Paste button) pastes, through the Mac clipboard in Go | v0.1.72 |
| 66 | Server Disconnect (closes terminals, files, chat and command links; tmux sessions keep running) and Reconnect in the server header | v0.1.74 |
| 35 | Fix for the blank window / React #300: ChatView called two hooks after its no-agent return, so the hook count changed when the agent arrived (introduced with the tool-call toggle in v0.1.24) | v0.1.42 |

## Now

| # | Item | Size | Notes |
| --- | --- | --- | --- |

## Next

| # | Item | Size | Notes |
| --- | --- | --- | --- |

## Later

| # | Item | Size | Notes |
| --- | --- | --- | --- |
| 14 | **Desktop control gaps.** Non-US keyboard layouts, file transfer. Secondary displays and clipboard sync shipped in v0.1.20. | M | |
| 17 | **Linux capture and input.** | L | |
| 21 | **A `clawhq` gateway plugin.** Phase 1 shipped in v0.1.23: `plugin/` (methods, tools with real caller id, org context, inbox events, activity records) and the ClawHQ bridge (detect, org mirror and import, notice events, exec push). Left: publish to ClawHub, server-side search and usage summary, activity feed UI from `clawhq.activity`. | M | in progress |
| 23 | **ClawHQ data on the gateway.** OpenClaw already keeps its state in SQLite on the gateway host. Moving ClawHQ's own data (departments, notifications, exec audit) there, through the plugin, makes two installs behave as one system. | M | part of 21 |
