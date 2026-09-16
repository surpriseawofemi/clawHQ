# ClawHQ gateway plugin

An OpenClaw gateway plugin that gives every [ClawHQ](https://github.com/surpriseawofemi/clawHQ)
desktop one shared org chart, inbox and command history, and gives agents tools that
know who called them.

Install on the gateway host:

```bash
openclaw plugins install clawhq-openclaw-plugin
```

or from ClawHQ: Settings → Plugins → ClawHQ on the gateway → Install.

Then let it use the conversation hooks (ClawHQ does this for you when it installs):

```json
{ "plugins": { "entries": { "clawhq": { "hooks": { "allowConversationAccess": true, "allowPromptInjection": true } } } } }
```

## What it provides

Gateway methods (operator scope): `clawhq.version`, `clawhq.org.get`,
`clawhq.org.upsertDepartment`, `clawhq.org.removeDepartment`, `clawhq.org.assign`,
`clawhq.org.import`, `clawhq.inbox.list`, `clawhq.inbox.append`, `clawhq.inbox.markRead`,
`clawhq.inbox.delete`, `clawhq.exec.append`, `clawhq.exec.list`, `clawhq.activity.list`.

Agent tools: `clawhq_departments_list`, `clawhq_department_create`, `clawhq_agent_assign`,
`clawhq_ask_human`. The runtime supplies the calling agent's id; an agent cannot speak
as another.

Gateway events, to every connected operator: `clawhq.notice` (an agent asked for a
human), `clawhq.org.changed`, `clawhq.activity` (an agent run ended).

Hooks: `agent_end` records what each agent did; `before_prompt_build` tells an agent
which department it is in (switch off with `orgContext: false` in the plugin config).

State lives in `<state dir>/clawhq/state.json`, written atomically. It is small by
design: the gateway's own SQLite keeps the conversations.

## Developing

```bash
cd plugin && npm install && npm run build
openclaw plugins install "$PWD" --force --accept-capabilities
```

Restart the gateway after installing. `openclaw plugins doctor` reports contract or
hook policy problems.
