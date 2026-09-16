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
};

const LIMITS = { notices: 1000, execLog: 5000, activity: 2000, tasks: 2000, team: 2000, issues: 2000 };

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
});

export class Store {
  private state: State | null = null;
  private chain: Promise<void> = Promise.resolve();
  readonly file: string;

  constructor(dir?: string) {
    const base = dir ?? process.env.OPENCLAW_STATE_DIR ?? path.join(os.homedir(), ".openclaw");
    this.file = path.join(base, "clawhq", "state.json");
  }

  async load(): Promise<State> {
    if (this.state) return this.state;
    try {
      const raw = await fs.readFile(this.file, "utf8");
      const parsed = JSON.parse(raw) as Partial<State>;
      this.state = { ...empty(), ...parsed };
    } catch {
      this.state = empty();
    }
    return this.state;
  }

  /** Applies a change under a write lock and persists the result. */
  async update<T>(fn: (s: State) => T): Promise<T> {
    let out!: T;
    this.chain = this.chain.then(async () => {
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
      await fs.mkdir(path.dirname(this.file), { recursive: true });
      const tmp = `${this.file}.tmp`;
      await fs.writeFile(tmp, JSON.stringify(s), "utf8");
      await fs.rename(tmp, this.file);
    });
    await this.chain;
    return out;
  }
}

export const newId = (prefix: string): string =>
  `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
