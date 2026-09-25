#!/usr/bin/env node
// ClawHQ server helper. One file, no dependencies. Installed by ClawHQ at
// ~/.clawhq/helper.js on a server; runs under the Node that Claude Code uses.
//
//   helper.js hook <event>            Claude Code hook: reads the hook JSON on stdin,
//                                     appends what happened to ~/.clawhq/outbox.jsonl
//   helper.js mcp --project <dir>     MCP server (stdio) with the ClawHQ tools
//   helper.js version
'use strict'
const fs = require('fs')
const os = require('os')
const path = require('path')

const VERSION = '__CLAWHQ_HELPER_VERSION__'
const HOME = os.homedir()
const BASE = path.join(HOME, '.clawhq')
const OUTBOX = path.join(BASE, 'outbox.jsonl')

function ensureBase() {
  fs.mkdirSync(BASE, { recursive: true })
}

function outbox(ev) {
  ensureBase()
  fs.appendFileSync(OUTBOX, JSON.stringify({ ts: Date.now(), ...ev }) + '\n')
}

// Subscription (OAuth credentials file) or API key: ClawHQ shows window usage
// for the first and cost for the second.
function billing() {
  if (process.env.ANTHROPIC_API_KEY) return 'api'
  try {
    if (fs.statSync(path.join(HOME, '.claude', '.credentials.json')).size > 0) return 'subscription'
  } catch {}
  return 'unknown'
}

function readStdin() {
  try {
    return fs.readFileSync(0, 'utf8')
  } catch {
    return ''
  }
}

// ---- hooks ---------------------------------------------------------------

function lastAssistant(transcriptPath) {
  let text = ''
  let usage = null
  let model = ''
  let files = new Set()
  try {
    const lines = fs.readFileSync(transcriptPath, 'utf8').split('\n')
    for (const line of lines) {
      if (!line.trim()) continue
      let rec
      try {
        rec = JSON.parse(line)
      } catch {
        continue
      }
      if (rec.type === 'assistant' && rec.message) {
        const c = rec.message.content
        if (Array.isArray(c)) {
          const t = c.filter((b) => b && b.type === 'text' && b.text).map((b) => b.text).join('\n')
          if (t.trim()) text = t
          for (const b of c) {
            if (b && b.type === 'tool_use' && b.input) {
              const p = b.input.file_path || b.input.path
              if (p && (b.name === 'Edit' || b.name === 'Write' || b.name === 'MultiEdit' || b.name === 'NotebookEdit')) files.add(String(p))
            }
          }
        } else if (typeof c === 'string' && c.trim()) text = c
        if (rec.message.usage) usage = rec.message.usage
        if (rec.message.model) model = rec.message.model
      }
    }
  } catch {}
  return { text, usage, model, files: [...files] }
}

function hook(event) {
  let input = {}
  try {
    input = JSON.parse(readStdin() || '{}')
  } catch {}
  const info = input.transcript_path ? lastAssistant(input.transcript_path) : { text: '', usage: null, model: '', files: [] }
  outbox({
    type: event === 'SessionEnd' ? 'session-end' : 'reply',
    event,
    project: input.cwd || process.cwd(),
    sessionId: input.session_id || '',
    text: (info.text || '').slice(0, 20000),
    model: info.model,
    usage: info.usage ? { input: info.usage.input_tokens || 0, output: info.usage.output_tokens || 0, cacheRead: info.usage.cache_read_input_tokens || 0 } : null,
    files: info.files.slice(0, 50),
    billing: billing(),
  })
  // Never block Claude Code: exit 0, no output.
  process.exit(0)
}

// ---- issues and log in the project ----------------------------------------

function projectPaths(dir) {
  const root = path.join(dir, '.clawhq')
  return { root, issues: path.join(root, 'issues.json'), details: path.join(root, 'issues'), recs: path.join(root, 'recommendations'), mission: path.join(root, 'MISSION.md'), log: path.join(root, 'log.jsonl') }
}

function loadIssues(dir) {
  const p = projectPaths(dir)
  let db
  try {
    db = JSON.parse(fs.readFileSync(p.issues, 'utf8'))
  } catch {
    db = { next: 1, issues: [] }
  }
  // Recommendations share the file: optional ideas the boss may take or leave.
  if (!db.nextRec) db.nextRec = 1
  if (!Array.isArray(db.recommendations)) db.recommendations = []
  return db
}

function saveIssues(dir, db) {
  const p = projectPaths(dir)
  fs.mkdirSync(p.details, { recursive: true })
  const tmp = p.issues + '.tmp'
  fs.writeFileSync(tmp, JSON.stringify(db, null, 2))
  fs.renameSync(tmp, p.issues)
}

function appendLog(dir, ev) {
  const p = projectPaths(dir)
  fs.mkdirSync(p.root, { recursive: true })
  fs.appendFileSync(p.log, JSON.stringify({ ts: Date.now(), ...ev }) + '\n')
  outbox({ project: dir, ...ev })
}

const TOOLS = [
  {
    name: 'clawhq_log',
    description: 'Log one short line to ClawHQ (the boss reads these in the app, hourly). One sentence: what was checked, what was changed, or what was found. Details belong in an issue, not here.',
    inputSchema: { type: 'object', properties: { text: { type: 'string', description: 'One short line' }, kind: { type: 'string', description: 'info (default), change, or warn' } }, required: ['text'] },
  },
  {
    name: 'clawhq_issue',
    description: 'Open a numbered issue for the boss in ClawHQ. Use it for anything that needs a decision, approval or is too risky to just do. The number comes back; write the long details to the file path returned so you can answer "#N" questions later. Keep the title to one line.',
    inputSchema: {
      type: 'object',
      properties: {
        title: { type: 'string', description: 'One line' },
        details: { type: 'string', description: 'Everything you want to remember about it: findings, options, your recommendation, files involved. Markdown.' },
        needs_boss: { type: 'boolean', description: 'true when the boss must decide or approve; false for a note to yourself' },
        urgency: { type: 'string', description: 'low, normal, high or urgent' },
      },
      required: ['title'],
    },
  },
  {
    name: 'clawhq_issue_update',
    description: 'Update a numbered issue: add a note, change status (open, done, dismissed) or append to its details file.',
    inputSchema: { type: 'object', properties: { n: { type: 'number' }, status: { type: 'string' }, note: { type: 'string' }, details: { type: 'string', description: 'Markdown appended to the details file' } }, required: ['n'] },
  },
  {
    name: 'clawhq_issues',
    description: 'List issues (open by default) with their numbers, and read one issue\'s details file by number.',
    inputSchema: { type: 'object', properties: { n: { type: 'number', description: 'Read this issue\'s details' }, status: { type: 'string', description: 'open, done, dismissed or all' } } },
  },
  {
    name: 'clawhq_recommend',
    description: 'Suggest something optional to the boss: an improvement worth considering that is not broken, risky or blocking. Not for problems; those are issues. Returns R-number and a details file. Before suggesting, call clawhq_recommendations with status "all" and never repeat one that was dismissed.',
    inputSchema: {
      type: 'object',
      properties: {
        title: { type: 'string', description: 'One line' },
        why: { type: 'string', description: 'One paragraph: what it would improve and roughly how. Markdown.' },
        effort: { type: 'string', description: 'small, medium or large' },
      },
      required: ['title'],
    },
  },
  {
    name: 'clawhq_recommendations',
    description: 'List recommendations (open by default) with their R-numbers, or read one by number. Status: open, later, accepted, dismissed or all.',
    inputSchema: { type: 'object', properties: { r: { type: 'number', description: 'Read this recommendation' }, status: { type: 'string' } } },
  },
  {
    name: 'clawhq_mission',
    description: 'Read the standing mission the boss set for this project in ClawHQ (the rotation of focus areas, the rules for what you may change without asking, how to log).',
    inputSchema: { type: 'object', properties: {} },
  },
]

function callTool(dir, name, a) {
  a = a || {}
  const p = projectPaths(dir)
  switch (name) {
    case 'clawhq_log': {
      const text = String(a.text || '').trim().slice(0, 500)
      if (!text) return { error: 'text is required' }
      appendLog(dir, { type: 'log', kind: a.kind || 'info', text })
      return { ok: true }
    }
    case 'clawhq_issue': {
      const db = loadIssues(dir)
      const n = db.next || 1
      const title = String(a.title || '').trim().slice(0, 200)
      if (!title) return { error: 'title is required' }
      const issue = { n, title, status: 'open', needsBoss: a.needs_boss !== false, urgency: ['low', 'normal', 'high', 'urgent'].includes(a.urgency) ? a.urgency : 'normal', createdAt: Date.now(), updatedAt: Date.now(), notes: [] }
      db.next = n + 1
      db.issues.push(issue)
      saveIssues(dir, db)
      const file = path.join(p.details, `${n}.md`)
      fs.writeFileSync(file, `# #${n} ${title}\n\n${a.details || ''}\n`)
      appendLog(dir, { type: 'issue', n, title, needsBoss: issue.needsBoss, urgency: issue.urgency })
      return { ok: true, n, details_file: file }
    }
    case 'clawhq_issue_update': {
      const db = loadIssues(dir)
      const issue = db.issues.find((x) => x.n === Number(a.n))
      if (!issue) return { error: `no issue #${a.n}` }
      if (a.status && ['open', 'done', 'dismissed'].includes(a.status)) issue.status = a.status
      if (a.note) issue.notes.push({ ts: Date.now(), text: String(a.note).slice(0, 2000) })
      issue.updatedAt = Date.now()
      saveIssues(dir, db)
      if (a.details) fs.appendFileSync(path.join(p.details, `${issue.n}.md`), `\n${a.details}\n`)
      appendLog(dir, { type: 'issue-update', n: issue.n, status: issue.status, title: issue.title, note: a.note ? String(a.note).slice(0, 500) : undefined })
      return { ok: true, n: issue.n, status: issue.status }
    }
    case 'clawhq_issues': {
      const db = loadIssues(dir)
      if (a.n) {
        const issue = db.issues.find((x) => x.n === Number(a.n))
        if (!issue) return { error: `no issue #${a.n}` }
        let details = ''
        try {
          details = fs.readFileSync(path.join(p.details, `${issue.n}.md`), 'utf8')
        } catch {}
        return { issue, details }
      }
      const status = a.status || 'open'
      return { issues: db.issues.filter((x) => status === 'all' || x.status === status).map((x) => ({ n: x.n, title: x.title, status: x.status, needsBoss: x.needsBoss, urgency: x.urgency })) }
    }
    case 'clawhq_recommend': {
      const db = loadIssues(dir)
      const r = db.nextRec
      const title = String(a.title || '').trim().slice(0, 200)
      if (!title) return { error: 'title is required' }
      const effort = ['small', 'medium', 'large'].includes(a.effort) ? a.effort : 'medium'
      const rec = { r, title, effort, status: 'open', createdAt: Date.now(), updatedAt: Date.now() }
      db.nextRec = r + 1
      db.recommendations.push(rec)
      saveIssues(dir, db)
      fs.mkdirSync(p.recs, { recursive: true })
      const file = path.join(p.recs, `${r}.md`)
      fs.writeFileSync(file, `# R${r} ${title}\n\n${a.why || ''}\n`)
      appendLog(dir, { type: 'recommendation', r, title, effort })
      return { ok: true, r, details_file: file }
    }
    case 'clawhq_recommendations': {
      const db = loadIssues(dir)
      if (a.r) {
        const rec = db.recommendations.find((x) => x.r === Number(a.r))
        if (!rec) return { error: `no recommendation R${a.r}` }
        let details = ''
        try {
          details = fs.readFileSync(path.join(p.recs, `${rec.r}.md`), 'utf8')
        } catch {}
        return { recommendation: rec, details }
      }
      const status = a.status || 'open'
      return { recommendations: db.recommendations.filter((x) => status === 'all' || x.status === status).map((x) => ({ r: x.r, title: x.title, status: x.status, effort: x.effort, issue: x.issueN })) }
    }
    case 'clawhq_mission': {
      try {
        return { mission: fs.readFileSync(p.mission, 'utf8') }
      } catch {
        return { mission: '', note: 'no mission set yet in ClawHQ' }
      }
    }
    default:
      return { error: `unknown tool ${name}` }
  }
}

// ---- MCP over stdio (JSON-RPC 2.0, newline-delimited) ----------------------

function mcp(dir) {
  let buf = ''
  const send = (msg) => process.stdout.write(JSON.stringify(msg) + '\n')
  process.stdin.setEncoding('utf8')
  process.stdin.on('data', (chunk) => {
    buf += chunk
    let i
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i).trim()
      buf = buf.slice(i + 1)
      if (!line) continue
      let req
      try {
        req = JSON.parse(line)
      } catch {
        continue
      }
      handle(req)
    }
  })
  function handle(req) {
    const id = req.id
    const reply = (result) => id !== undefined && send({ jsonrpc: '2.0', id, result })
    const fail = (code, message) => id !== undefined && send({ jsonrpc: '2.0', id, error: { code, message } })
    switch (req.method) {
      case 'initialize':
        return reply({ protocolVersion: req.params && req.params.protocolVersion ? req.params.protocolVersion : '2024-11-05', capabilities: { tools: {} }, serverInfo: { name: 'clawhq', version: VERSION } })
      case 'notifications/initialized':
      case 'notifications/cancelled':
        return
      case 'ping':
        return reply({})
      case 'tools/list':
        return reply({ tools: TOOLS })
      case 'tools/call': {
        const name = req.params && req.params.name
        const out = callTool(dir, name, req.params && req.params.arguments)
        return reply({ content: [{ type: 'text', text: JSON.stringify(out) }], isError: Boolean(out && out.error) })
      }
      default:
        return fail(-32601, `method not found: ${req.method}`)
    }
  }
}

// ---- main -------------------------------------------------------------------

const [cmd, ...rest] = process.argv.slice(2)
if (cmd === 'hook') hook(rest[0] || 'Stop')
else if (cmd === 'mcp') {
  const i = rest.indexOf('--project')
  const dir = i >= 0 && rest[i + 1] ? rest[i + 1] : process.cwd()
  mcp(dir)
} else if (cmd === 'version') {
  process.stdout.write(VERSION + '\n')
} else {
  process.stderr.write('usage: helper.js hook <event> | mcp --project <dir> | version\n')
  process.exit(2)
}
