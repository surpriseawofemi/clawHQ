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
