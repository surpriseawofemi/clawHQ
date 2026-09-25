import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";

/**
 * ClawHQ's shared state on the gateway host: one JSON file, written whole and
 * atomically. The data is small (an org chart, a bounded inbox, bounded logs), so a
 * file beats a database dependency inside the gateway process.
 */

export type Department = { id: string; name: string; emoji: string; order: number };

export type Notice = {
  id: string;
  title: string;
  body: string;
  agentId?: string;
  agentName?: string;
  agentEmoji?: string;
  sessionKey?: string;
  origin?: string;
  atMs: number;
  read: boolean;
};

export type ExecRecord = {
  id: string;
  atMs: number;
  machine?: string;
  agentId?: string;
  sessionKey?: string;
  command: string;
  cwd?: string;
  decision: string;
  ran: boolean;
  exitCode?: number;
  success: boolean;
  timedOut?: boolean;
  durationMs?: number;
  output?: string;
  error?: string;
};

export type Activity = {
  id: string;
  atMs: number;
  agentId?: string;
  sessionKey?: string;
  runId?: string;
  success: boolean;
  durationMs?: number;
  error?: string;
  /** The last thing the agent said, trimmed. */
  summary?: string;
  channel?: string;
};

export type TaskStatus = "todo" | "doing" | "done" | "failed";

export type Task = {
  id: string;
  title: string;
  details?: string;
  agentId?: string;
  status: TaskStatus;
  createdAtMs: number;
  updatedAtMs: number;
  /** "human" or the agent id that created it. */
  createdBy: string;
  /** The thread the run happens in, once started. */
  sessionKey?: string;
  runId?: string;
  startedAtMs?: number;
  finishedAtMs?: number;
  result?: string;
  error?: string;
};

/** One post on the Team Chat board: the human or an agent, addressed to @ids or nobody. */
export type TeamPost = {
  id: string;
  atMs: number;
  from: string;
  fromKind: "human" | "agent";
  text: string;
  mentions: string[];
  sessionKey?: string;
  runId?: string;
  /** How many agent-to-agent replies led here; 0 for the human. Caps ping-pong. */
  hops?: number;
  /** The post this one answers, when it came from a turn ClawHQ started. */
  replyTo?: string;
};

/** Something an agent needs from the human (or a task the human gives an agent), answered one by one. */
export type IssueKind = "question" | "task" | "issue" | "improvement";
export type IssueStatus = "open" | "in-progress" | "resolved";
export type IssueUrgency = "low" | "normal" | "high" | "urgent";
export type IssueReply = { id: string; by: string; byKind: "human" | "agent"; text: string; atMs: number };
export type Issue = {
  id: string;
  kind: IssueKind;
  title: string;
  body?: string;
  from: string;
  fromKind: "human" | "agent";
  assigneeAgentId?: string;
  urgency: IssueUrgency;
  status: IssueStatus;
  createdAtMs: number;
  updatedAtMs: number;
  resolvedAtMs?: number;
  sessionKey?: string;
  replies: IssueReply[];
};

/** A ClawHQ's servers, as it registers them (names only, never credentials). */
export type ServerEntry = {
  id: string;
  name: string;
  /** One line from the operator about what the server is for. */
  description?: string;
  /** Agent ids that may hand it tasks; empty or missing means every agent. */
  agents?: string[];
  projects: { id: string; name: string; agent: string }[];
};
export type ServerRegistration = { clawhqId: string; atMs: number; servers: ServerEntry[] };

/** A task an agent handed to a server's coding agent; a ClawHQ runs it. */
export type ServerTask = {
  id: string;
  serverId: string;
  serverName: string;
  projectId?: string;
  task: string;
  from: string;
  status: "queued" | "running" | "done" | "failed";
  claimedBy?: string;
  result?: string;
  costUsd?: number;
  createdAtMs: number;
  updatedAtMs: number;
};

export type State = {
  version: number;
  departments: Department[];
  assignments: Record<string, string>;
  notices: Notice[];
  execLog: ExecRecord[];
  activity: Activity[];
  tasks: Task[];
  team: TeamPost[];
  /** Per agent: atMs of the newest board post it has been shown. */
  teamSeen: Record<string, number>;
  /** Per agent: the board post its current Team Chat turn answers. */
  teamTurn: Record<string, { postId: string; hops: number; atMs: number }>;
  issues: Issue[];
  serverRegistry: Record<string, ServerRegistration>;
  serverTasks: ServerTask[];
};

const LIMITS = { notices: 1000, execLog: 5000, activity: 2000, tasks: 2000, team: 2000, issues: 2000, serverTasks: 500 };

const empty = (): State => ({
  version: 1,
  departments: [],
  assignments: {},
  notices: [],
  execLog: [],
  activity: [],
  tasks: [],
  team: [],
  teamSeen: {},
  teamTurn: {},
  issues: [],
  serverRegistry: {},
  serverTasks: [],
});

export class Store {
  private chain: Promise<void> = Promise.resolve();
  readonly file: string;

  constructor(dir?: string) {
    const base = dir ?? process.env.OPENCLAW_STATE_DIR ?? path.join(os.homedir(), ".openclaw");
    this.file = path.join(base, "clawhq", "state.json");
  }

  /**
   * Always reads the file. The gateway and the tool bridge it spawns for CLI
   * backends each load this plugin, so two processes share one file; a cached
   * copy in either would go stale the moment the other writes, and a later
   * write from the stale side would erase the other's changes. (That happened:
   * issues filed through tools vanished while activity written by hooks
   * survived.)
   */
  async load(): Promise<State> {
    try {
      const raw = await fs.readFile(this.file, "utf8");
      const parsed = JSON.parse(raw) as Partial<State>;
      return { ...empty(), ...parsed };
    } catch {
      return empty();
    }
  }

  /**
   * Read-modify-write under a cross-process lock: a lock file created with
   * O_EXCL, retried for a few seconds, treated as stale after ten. Within one
   * process, calls also queue so they never interleave.
   */
  async update<T>(fn: (s: State) => T): Promise<T> {
    let out!: T;
    this.chain = this.chain.then(async () => {
      await fs.mkdir(path.dirname(this.file), { recursive: true });
      const lock = `${this.file}.lock`;
      await acquire(lock);
      try {
        const s = await this.load();
        out = fn(s);
        s.notices = s.notices.slice(-LIMITS.notices);
        s.execLog = s.execLog.slice(-LIMITS.execLog);
        s.activity = s.activity.slice(-LIMITS.activity);
        s.tasks = (s.tasks ?? []).slice(-LIMITS.tasks);
        s.team = (s.team ?? []).slice(-LIMITS.team);
        s.teamSeen = s.teamSeen ?? {};
        s.teamTurn = s.teamTurn ?? {};
        s.issues = (s.issues ?? []).slice(-LIMITS.issues);
      s.serverRegistry = s.serverRegistry ?? {};
      s.serverTasks = (s.serverTasks ?? []).slice(-LIMITS.serverTasks);
        const tmp = `${this.file}.${process.pid}.${Math.random().toString(36).slice(2, 8)}.tmp`;
        await fs.writeFile(tmp, JSON.stringify(s), "utf8");
        await fs.rename(tmp, this.file);
      } finally {
        await fs.rm(lock, { force: true }).catch(() => undefined);
      }
    });
    await this.chain;
    return out;
  }
}

const LOCK_STALE_MS = 10_000;
const LOCK_WAIT_MS = 5_000;

async function acquire(lock: string): Promise<void> {
  const started = Date.now();
  for (;;) {
    try {
      const h = await fs.open(lock, "wx");
      await h.writeFile(String(process.pid));
      await h.close();
      return;
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "EEXIST") throw err;
      try {
        const st = await fs.stat(lock);
        if (Date.now() - st.mtimeMs > LOCK_STALE_MS) {
          await fs.rm(lock, { force: true });
          continue;
        }
      } catch {
        continue; // vanished between the open and the stat
      }
      if (Date.now() - started > LOCK_WAIT_MS) throw new Error(`clawhq state is locked by another process (${lock})`);
      await new Promise((r) => setTimeout(r, 15 + Math.random() * 35));
    }
  }
}

export const newId = (prefix: string): string =>
  `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
