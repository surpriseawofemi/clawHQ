import { Type } from "@sinclair/typebox";
import { definePluginEntry, jsonResult } from "openclaw/plugin-sdk/core";
import type {
  GatewayRequestHandlerOptions,
  OpenClawPluginApi,
  OpenClawPluginToolContext,
} from "openclaw/plugin-sdk/core";
import { Store, newId } from "./store.js";
import type { Activity, Department, ExecRecord, Notice, State, Task, TaskStatus } from "./store.js";

/** Live state of one agent, kept in memory: what it is doing right now. */
type Presence = {
  agentId: string;
  state: "idle" | "working";
  sinceMs: number;
  sessionKey?: string;
  runId?: string;
  tool?: string;
  toolSinceMs?: number;
  lastLine?: string;
  lastEndMs?: number;
  lastSuccess?: boolean;
};

/** A parent agent waiting on a child it spawned. */
type Delegation = {
  childSessionKey: string;
  childAgentId: string;
  parentAgentId?: string;
  parentSessionKey?: string;
  label?: string;
  runId?: string;
  sinceMs: number;
};

const TASK_STATUSES: TaskStatus[] = ["todo", "doing", "done", "failed"];

export const PLUGIN_VERSION = "0.2.0";

/**
 * ClawHQ's gateway plugin.
 *
 * ClawHQ desktops keep their own machine-local concerns (running commands, screen,
 * clipboard). Everything that belongs to the org rather than to one machine lives
 * here, once: the department chart, the inbox of things agents asked a human for,
 * the command history from every machine, and a feed of what each agent did.
 * Agent tools registered here receive the caller's real agent id from the runtime,
 * so an agent cannot claim to be another.
 */

type Params = Record<string, unknown>;

const str = (p: Params, k: string): string => (typeof p[k] === "string" ? (p[k] as string).trim() : "");
const num = (p: Params, k: string, d: number): number => (typeof p[k] === "number" && Number.isFinite(p[k]) ? (p[k] as number) : d);

const slug = (s: string): string =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");

const fail = (opts: GatewayRequestHandlerOptions, message: string): void => {
  opts.respond(false, undefined, { code: "INVALID_REQUEST", message });
};

const ok = (opts: GatewayRequestHandlerOptions, payload: unknown): void => {
  opts.respond(true, payload);
};

const orgView = (s: State) => ({
  departments: [...s.departments].sort((a, b) => a.order - b.order),
  assignments: { ...s.assignments },
});

const lastAssistantText = (messages: unknown[]): string | undefined => {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i] as { role?: string; content?: unknown };
    if (m?.role !== "assistant") continue;
    if (typeof m.content === "string") return m.content.trim().slice(0, 300);
    if (Array.isArray(m.content)) {
      const text = m.content
        .filter((b) => b && typeof b === "object" && (b as { type?: string }).type === "text")
        .map((b) => String((b as { text?: string }).text ?? ""))
        .join("\n")
        .trim();
      if (text) return text.slice(0, 300);
    }
  }
  return undefined;
};

function register(api: OpenClawPluginApi): void {
  const store = new Store();
  const cfg = (api.pluginConfig ?? {}) as { orgContext?: boolean; activityLimit?: number };
  const orgContext = cfg.orgContext !== false;

  const emit = (event: string, payload: Record<string, unknown>): void => {
    try {
      const events = (api as unknown as { gatewayEvents?: { emit: (e: string, p: unknown, o: { scope: string }) => void } }).gatewayEvents;
      events?.emit(event, payload, { scope: "operator.read" });
    } catch (err) {
      api.logger.warn(`clawhq: event ${event} not emitted: ${String(err)}`);
    }
  };

  const upsertDepartment = async (d: Partial<Department>): Promise<Department> =>
    store.update((s) => {
      const id = slug(d.id || d.name || "");
      if (!id) throw new Error("a department needs a name");
      const cur = s.departments.find((x) => x.id === id);
      const next: Department = {
        id,
        name: (d.name ?? cur?.name ?? id).trim(),
        emoji: (d.emoji ?? cur?.emoji ?? "🏷️").trim() || "🏷️",
        order: typeof d.order === "number" ? d.order : cur?.order ?? s.departments.length,
      };
      if (cur) Object.assign(cur, next);
      else s.departments.push(next);
      return next;
    });

  const assign = async (agentId: string, departmentId: string): Promise<void> =>
    store.update((s) => {
      if (!departmentId) {
        delete s.assignments[agentId];
        return;
      }
      if (!s.departments.some((d) => d.id === departmentId)) throw new Error(`no department "${departmentId}"`);
      s.assignments[agentId] = departmentId;
    });

  const addNotice = async (n: Omit<Notice, "id" | "atMs" | "read"> & Partial<Pick<Notice, "id" | "atMs">>): Promise<Notice> => {
    const notice: Notice = { ...n, id: n.id ?? newId("n"), atMs: n.atMs ?? Date.now(), read: false };
    await store.update((s) => {
      if (!s.notices.some((x) => x.id === notice.id)) s.notices.push(notice);
    });
    emit("clawhq.notice", notice);
    return notice;
  };

  // ---- gateway methods, for ClawHQ desktops --------------------------------

  const method = (name: string, scope: "operator.read" | "operator.write", fn: (p: Params, opts: GatewayRequestHandlerOptions) => Promise<unknown>) => {
    api.registerGatewayMethod(
      name,
      async (opts) => {
        try {
          ok(opts, await fn(opts.params ?? {}, opts));
        } catch (err) {
          fail(opts, err instanceof Error ? err.message : String(err));
        }
      },
      { scope },
    );
  };

  method("clawhq.version", "operator.read", async () => ({
    version: PLUGIN_VERSION,
    features: ["org", "inbox", "exec", "activity", "events", "orgContext", "presence", "tasks"],
    stateFile: store.file,
  }));

  method("clawhq.org.get", "operator.read", async () => orgView(await store.load()));
  method("clawhq.org.upsertDepartment", "operator.write", async (p) => {
    await upsertDepartment({ id: str(p, "id"), name: str(p, "name"), emoji: str(p, "emoji"), order: typeof p.order === "number" ? (p.order as number) : undefined });
    return orgView(await store.load());
  });
  method("clawhq.org.removeDepartment", "operator.write", async (p) => {
    const id = str(p, "id");
    await store.update((s) => {
      s.departments = s.departments.filter((d) => d.id !== id);
      for (const [agent, dept] of Object.entries(s.assignments)) if (dept === id) delete s.assignments[agent];
    });
    return orgView(await store.load());
  });
  method("clawhq.org.assign", "operator.write", async (p) => {
    const agentId = str(p, "agentId");
    if (!agentId) throw new Error("agentId is required");
    await assign(agentId, str(p, "departmentId"));
    return orgView(await store.load());
  });
  // One-time import from a ClawHQ that kept the org locally. Fills gaps only.
  method("clawhq.org.import", "operator.write", async (p) => {
    const departments = Array.isArray(p.departments) ? (p.departments as Partial<Department>[]) : [];
    const assignments = p.assignments && typeof p.assignments === "object" ? (p.assignments as Record<string, string>) : {};
    let imported = 0;
    await store.update((s) => {
      for (const d of departments) {
        const id = slug(d.id || d.name || "");
        if (!id || s.departments.some((x) => x.id === id)) continue;
        s.departments.push({ id, name: (d.name ?? id).trim(), emoji: (d.emoji ?? "🏷️").trim() || "🏷️", order: typeof d.order === "number" ? d.order : s.departments.length });
        imported++;
      }
      for (const [agent, dept] of Object.entries(assignments)) {
        if (!s.assignments[agent] && s.departments.some((x) => x.id === dept)) {
          s.assignments[agent] = dept;
          imported++;
        }
      }
    });
    return { imported, ...orgView(await store.load()) };
  });

  method("clawhq.inbox.list", "operator.read", async (p) => {
    const s = await store.load();
    const since = num(p, "sinceMs", 0);
    const limit = Math.min(1000, Math.max(1, num(p, "limit", 500)));
    const list = s.notices.filter((n) => n.atMs > since).slice(-limit).reverse();
    return { notices: list, unread: s.notices.filter((n) => !n.read).length };
  });
  method("clawhq.inbox.append", "operator.write", async (p) => {
    const n = p.notice as Partial<Notice> | undefined;
    if (!n || typeof n.body !== "string") throw new Error("notice.body is required");
    return addNotice({ title: n.title ?? "", body: n.body, agentId: n.agentId, agentName: n.agentName, agentEmoji: n.agentEmoji, sessionKey: n.sessionKey, origin: n.origin, id: n.id, atMs: n.atMs });
  });
  method("clawhq.inbox.markRead", "operator.write", async (p) => {
    const id = str(p, "id");
    await store.update((s) => {
      for (const n of s.notices) if (!id || n.id === id) n.read = true;
    });
    return { ok: true };
  });
  method("clawhq.inbox.delete", "operator.write", async (p) => {
    const id = str(p, "id");
    await store.update((s) => {
      s.notices = id ? s.notices.filter((n) => n.id !== id) : [];
    });
    return { ok: true };
  });

  method("clawhq.exec.append", "operator.write", async (p) => {
    const r = p.record as Partial<ExecRecord> | undefined;
    if (!r || typeof r.command !== "string") throw new Error("record.command is required");
    const rec: ExecRecord = {
      id: r.id ?? newId("x"),
      atMs: r.atMs ?? Date.now(),
      machine: r.machine,
      agentId: r.agentId,
      sessionKey: r.sessionKey,
      command: r.command,
      cwd: r.cwd,
      decision: r.decision ?? "unknown",
      ran: r.ran === true,
      exitCode: r.exitCode,
      success: r.success === true,
      timedOut: r.timedOut,
      durationMs: r.durationMs,
      output: typeof r.output === "string" ? r.output.slice(-4000) : undefined,
      error: r.error,
    };
    await store.update((s) => {
      if (!s.execLog.some((x) => x.id === rec.id)) s.execLog.push(rec);
    });
    return { ok: true, id: rec.id };
  });
  method("clawhq.exec.list", "operator.read", async (p) => {
    const s = await store.load();
    const agentId = str(p, "agentId");
    const machine = str(p, "machine");
    const limit = Math.min(2000, Math.max(1, num(p, "limit", 500)));
    const list = s.execLog.filter((r) => (!agentId || r.agentId === agentId) && (!machine || r.machine === machine)).slice(-limit).reverse();
    return { records: list, machines: [...new Set(s.execLog.map((r) => r.machine).filter(Boolean))] };
  });

  method("clawhq.activity.list", "operator.read", async (p) => {
    const s = await store.load();
    const agentId = str(p, "agentId");
    const since = num(p, "sinceMs", 0);
    const limit = Math.min(2000, Math.max(1, num(p, "limit", 200)));
    return { activity: s.activity.filter((a) => (!agentId || a.agentId === agentId) && a.atMs > since).slice(-limit).reverse() };
  });

  // ---- presence: who is doing what right now --------------------------------
  // In memory only; it is a picture of the moment, rebuilt from hooks as they fire.

  const presence = new Map<string, Presence>();
  const delegations = new Map<string, Delegation>();
  let presenceTimer: NodeJS.Timeout | null = null;

  const presenceView = () => ({
    agents: [...presence.values()],
    delegations: [...delegations.values()],
    atMs: Date.now(),
  });

  /** Coalesces bursts (tool calls come fast) into one event every 400 ms. */
  const presenceChanged = (): void => {
    if (presenceTimer) return;
    presenceTimer = setTimeout(() => {
      presenceTimer = null;
      emit("clawhq.presence", presenceView());
    }, 400);
  };

  const touch = (agentId: string | undefined, patch: Partial<Presence>): void => {
    if (!agentId) return;
    const cur = presence.get(agentId) ?? { agentId, state: "idle" as const, sinceMs: Date.now() };
    presence.set(agentId, { ...cur, ...patch });
    presenceChanged();
  };

  method("clawhq.presence.get", "operator.read", async () => presenceView());

  // ---- tasks: a board every ClawHQ and every agent can see --------------------

  const findTask = (s: State, id: string): Task => {
    const t = (s.tasks ?? []).find((x) => x.id === id);
    if (!t) throw new Error(`no task "${id}"`);
    return t;
  };

  const createTask = async (input: { title: string; details?: string; agentId?: string; createdBy: string }): Promise<Task> => {
    const title = input.title.trim();
    if (!title) throw new Error("a task needs a title");
    const now = Date.now();
    const task: Task = {
      id: newId("t"),
      title,
      details: input.details?.trim() || undefined,
      agentId: input.agentId?.trim() || undefined,
      status: "todo",
      createdAtMs: now,
      updatedAtMs: now,
      createdBy: input.createdBy,
    };
    await store.update((s) => {
      s.tasks = s.tasks ?? [];
      s.tasks.push(task);
    });
    emit("clawhq.tasks.changed", { id: task.id, status: task.status });
    return task;
  };

  const updateTask = async (id: string, patch: Partial<Task>): Promise<Task> => {
    let out!: Task;
    await store.update((s) => {
      const t = findTask(s, id);
      if (patch.status && !TASK_STATUSES.includes(patch.status)) throw new Error(`unknown status "${patch.status}"`);
      const now = Date.now();
      if (patch.status === "doing" && t.status !== "doing") t.startedAtMs = now;
      if ((patch.status === "done" || patch.status === "failed") && t.status !== patch.status) t.finishedAtMs = now;
      Object.assign(t, patch, { updatedAtMs: now });
      out = { ...t };
    });
    emit("clawhq.tasks.changed", { id, status: out.status });
    return out;
  };

  method("clawhq.tasks.list", "operator.read", async (p) => {
    const s = await store.load();
    const agentId = str(p, "agentId");
    const status = str(p, "status");
    const since = num(p, "sinceMs", 0);
    const list = (s.tasks ?? []).filter((t) => (!agentId || t.agentId === agentId) && (!status || t.status === status) && t.updatedAtMs > since);
    return { tasks: [...list].sort((a, b) => b.updatedAtMs - a.updatedAtMs) };
  });
  method("clawhq.tasks.create", "operator.write", async (p) => ({
    task: await createTask({ title: str(p, "title"), details: str(p, "details"), agentId: str(p, "agentId"), createdBy: "human" }),
  }));
  method("clawhq.tasks.update", "operator.write", async (p) => {
    const id = str(p, "id");
    const patch: Partial<Task> = {};
    if (typeof p.title === "string") patch.title = p.title.trim();
    if (typeof p.details === "string") patch.details = p.details.trim() || undefined;
    if (typeof p.agentId === "string") patch.agentId = p.agentId.trim() || undefined;
    if (typeof p.status === "string") patch.status = p.status as TaskStatus;
    if (typeof p.result === "string") patch.result = p.result;
    if (typeof p.error === "string") patch.error = p.error;
    if (typeof p.sessionKey === "string") patch.sessionKey = p.sessionKey;
    if (typeof p.runId === "string") patch.runId = p.runId;
    return { task: await updateTask(id, patch) };
  });
  method("clawhq.tasks.delete", "operator.write", async (p) => {
    const id = str(p, "id");
    await store.update((s) => {
      s.tasks = (s.tasks ?? []).filter((t) => t.id !== id);
    });
    emit("clawhq.tasks.changed", { id, status: "deleted" });
    return { ok: true };
  });

  // ---- agent tools, with the caller's real identity ------------------------

  const describeDept = (s: State, id: string, agents: string[]) => {
    const d = s.departments.find((x) => x.id === id);
    return d ? { ...d, agents } : null;
  };

  api.registerTool(
    (ctx: OpenClawPluginToolContext) => [
      {
        name: "clawhq_departments_list",
        label: "ClawHQ departments",
        description: "List ClawHQ departments and which agents belong to each. Your own department is marked.",
        parameters: Type.Object({}),
        async execute() {
          const s = await store.load();
          const departments = [...s.departments]
            .sort((a, b) => a.order - b.order)
            .map((d) => ({ ...d, agents: Object.entries(s.assignments).filter(([, dept]) => dept === d.id).map(([a]) => a) }));
          const unassigned = ctx.agentId && !s.assignments[ctx.agentId];
          return jsonResult({ you: ctx.agentId ?? null, yourDepartment: ctx.agentId ? s.assignments[ctx.agentId] ?? null : null, unassigned, departments });
        },
      },
      {
        name: "clawhq_department_create",
        label: "Create ClawHQ department",
        description: "Create a ClawHQ department, or update its name or emoji if it already exists.",
        parameters: Type.Object({
          name: Type.String({ description: "Display name, e.g. Research" }),
          emoji: Type.Optional(Type.String({ description: "Optional emoji for the sidebar" })),
        }),
        async execute(_id: string, params: { name: string; emoji?: string }) {
          const d = await upsertDepartment({ name: params.name, emoji: params.emoji });
          emit("clawhq.org.changed", { by: ctx.agentId ?? null });
          return jsonResult({ ok: true, department: d });
        },
      },
      {
        name: "clawhq_agent_assign",
        label: "Assign agent to department",
        description: "Put an agent (yourself, or another by id) into a ClawHQ department. An empty departmentId unassigns.",
        parameters: Type.Object({
          agentId: Type.Optional(Type.String({ description: "Agent id; defaults to you" })),
          departmentId: Type.String({ description: "Department id from clawhq_departments_list" }),
        }),
        async execute(_id: string, params: { agentId?: string; departmentId: string }) {
          const target = params.agentId?.trim() || ctx.agentId;
          if (!target) throw new Error("no agent id");
          await assign(target, params.departmentId.trim());
          emit("clawhq.org.changed", { by: ctx.agentId ?? null });
          const s = await store.load();
          return jsonResult({ ok: true, agentId: target, department: describeDept(s, params.departmentId, Object.entries(s.assignments).filter(([, d]) => d === params.departmentId).map(([a]) => a)) });
        },
      },
      {
        name: "clawhq_ask_human",
        label: "Ask a human",
        description:
          "Ask the people running ClawHQ for help: to log in somewhere, approve something, or answer a question. " +
          "Every ClawHQ desktop shows it as a notification that names you. You cannot wait for the answer here; they will reply in your thread.",
        parameters: Type.Object({
          title: Type.Optional(Type.String({ description: "Short headline, e.g. Login needed" })),
          body: Type.String({ description: "What you need the person to do" }),
        }),
        async execute(_id: string, params: { title?: string; body: string }) {
          const n = await addNotice({ title: params.title ?? "", body: params.body, agentId: ctx.agentId, sessionKey: ctx.sessionKey, origin: "gateway" });
          return jsonResult({ ok: true, noticeId: n.id, delivered: "every connected ClawHQ" });
        },
      },
      {
        name: "clawhq_tasks_list",
        label: "ClawHQ tasks",
        description: "List tasks on the ClawHQ board. By default your own open tasks; pass all=true for everyone's, or a status to filter.",
        parameters: Type.Object({
          all: Type.Optional(Type.Boolean({ description: "Every agent's tasks, not only yours" })),
          status: Type.Optional(Type.String({ description: "todo, doing, done or failed" })),
        }),
        async execute(_id: string, params: { all?: boolean; status?: string }) {
          const s = await store.load();
          const list = (s.tasks ?? []).filter(
            (t) => (params.all || t.agentId === ctx.agentId) && (params.status ? t.status === params.status : t.status !== "done"),
          );
          return jsonResult({ you: ctx.agentId ?? null, tasks: list.map((t) => ({ id: t.id, title: t.title, details: t.details, agentId: t.agentId, status: t.status, result: t.result })) });
        },
      },
      {
        name: "clawhq_task_create",
        label: "Create ClawHQ task",
        description: "Put a task on the ClawHQ board for an agent (yourself or another by id). The people running ClawHQ see it, and the agent can pick it up.",
        parameters: Type.Object({
          title: Type.String({ description: "Short title" }),
          details: Type.Optional(Type.String({ description: "What exactly to do" })),
          agentId: Type.Optional(Type.String({ description: "Who should do it; defaults to you" })),
        }),
        async execute(_id: string, params: { title: string; details?: string; agentId?: string }) {
          const t = await createTask({ title: params.title, details: params.details, agentId: params.agentId?.trim() || ctx.agentId, createdBy: ctx.agentId ?? "agent" });
          return jsonResult({ ok: true, task: { id: t.id, title: t.title, agentId: t.agentId, status: t.status } });
        },
      },
      {
        name: "clawhq_task_update",
        label: "Update ClawHQ task",
        description: "Move a task on the ClawHQ board: doing when you start, done with a short result when finished, failed with an error if you cannot.",
        parameters: Type.Object({
          id: Type.String({ description: "Task id" }),
          status: Type.Optional(Type.String({ description: "todo, doing, done or failed" })),
          result: Type.Optional(Type.String({ description: "What was done, a few lines" })),
          error: Type.Optional(Type.String({ description: "Why it failed" })),
        }),
        async execute(_id: string, params: { id: string; status?: string; result?: string; error?: string }) {
          const patch: Partial<Task> = {};
          if (params.status) patch.status = params.status as TaskStatus;
          if (params.result) patch.result = params.result;
          if (params.error) patch.error = params.error;
          if (ctx.sessionKey && params.status === "doing") patch.sessionKey = ctx.sessionKey;
          const t = await updateTask(params.id, patch);
          return jsonResult({ ok: true, task: { id: t.id, status: t.status } });
        },
      },
    ],
    {
      names: [
        "clawhq_departments_list",
        "clawhq_department_create",
        "clawhq_agent_assign",
        "clawhq_ask_human",
        "clawhq_tasks_list",
        "clawhq_task_create",
        "clawhq_task_update",
      ],
    },
  );

  // ---- hooks ----------------------------------------------------------------

  api.on("agent_turn_prepare", async (_event, ctx) => {
    touch(ctx.agentId, { state: "working", sinceMs: Date.now(), sessionKey: ctx.sessionKey, runId: ctx.runId, tool: undefined });
  });

  api.on("before_tool_call", async (event, ctx) => {
    touch(ctx.agentId, { state: "working", tool: event.toolName, toolSinceMs: Date.now(), sessionKey: ctx.sessionKey ?? presence.get(ctx.agentId ?? "")?.sessionKey });
  });

  api.on("after_tool_call", async (_event, ctx) => {
    touch(ctx.agentId, { tool: undefined });
  });

  api.on("subagent_spawned", async (event) => {
    const requester = event.requester as { agentId?: string; sessionKey?: string } | undefined;
    delegations.set(event.childSessionKey, {
      childSessionKey: event.childSessionKey,
      childAgentId: event.agentId,
      parentAgentId: requester?.agentId,
      parentSessionKey: requester?.sessionKey,
      label: event.label,
      runId: event.runId,
      sinceMs: Date.now(),
    });
    touch(event.agentId, { state: "working", sinceMs: Date.now(), sessionKey: event.childSessionKey, runId: event.runId });
    presenceChanged();
  });

  api.on("subagent_ended", async (event) => {
    delegations.delete(event.targetSessionKey);
    presenceChanged();
  });

  api.on("agent_end", async (event, ctx) => {
    const line = lastAssistantText(event.messages ?? []);
    touch(ctx.agentId, { state: "idle", sinceMs: Date.now(), tool: undefined, lastLine: line, lastEndMs: Date.now(), lastSuccess: event.success });
    // A task that was being run in this thread finishes with the run.
    if (ctx.sessionKey) {
      try {
        const s = await store.load();
        const open = (s.tasks ?? []).find((t) => t.status === "doing" && t.sessionKey === ctx.sessionKey);
        if (open) {
          await updateTask(open.id, event.success ? { status: "done", result: open.result ?? line } : { status: "failed", error: open.error ?? event.error ?? "the run failed" });
        }
      } catch (err) {
        api.logger.warn(`clawhq: task not closed: ${String(err)}`);
      }
    }
    try {
      const rec: Activity = {
        id: newId("a"),
        atMs: Date.now(),
        agentId: ctx.agentId,
        sessionKey: ctx.sessionKey,
        runId: ctx.runId ?? event.runId,
        success: event.success,
        durationMs: event.durationMs,
        error: event.error,
        summary: lastAssistantText(event.messages ?? []),
        channel: ctx.channel,
      };
      await store.update((s) => {
        s.activity.push(rec);
      });
      emit("clawhq.activity", rec as unknown as Record<string, unknown>);
    } catch (err) {
      api.logger.warn(`clawhq: activity not recorded: ${String(err)}`);
    }
  });

  if (orgContext) {
    api.on("before_prompt_build", async (_event, ctx) => {
      if (!ctx.agentId) return;
      const s = await store.load();
      const deptId = s.assignments[ctx.agentId];
      if (!deptId) return;
      const dept = s.departments.find((d) => d.id === deptId);
      if (!dept) return;
      const peers = Object.entries(s.assignments)
        .filter(([a, d]) => d === deptId && a !== ctx.agentId)
        .map(([a]) => a);
      const line = `You are in the ${dept.name} department of this organisation (ClawHQ).${peers.length ? ` Also in ${dept.name}: ${peers.join(", ")}.` : ""} Use clawhq_departments_list to see the whole chart and clawhq_ask_human when you need a person.`;
      return { appendSystemContext: line };
    });
  }

  api.registerService({
    id: "clawhq",
    start: async () => {
      const s = await store.load();
      api.logger.info(`clawhq ${PLUGIN_VERSION}: ${s.departments.length} departments, ${s.notices.length} notices, state at ${store.file}`);
    },
    stop: () => undefined,
  });
}

export default definePluginEntry({
  id: "clawhq",
  name: "ClawHQ",
  description: "One org chart, inbox and command history for every ClawHQ, plus agent tools that know who called them.",
  register,
});
