package node

import (
	"context"
	"time"
)

// pluginTools are ClawHQ's agent-visible tools.
//
// IMPORTANT: these do not register yet, and the reason is structural. A published
// descriptor is silently dropped unless its backing `command` is already in the
// gateway's node command allowlist. Verified by experiment: an otherwise identical
// descriptor backed by `fs.listDir` registered, while these backed by `clawhq.*`
// did not, and `nodePluginTools` stayed empty.
//
// So a node cannot introduce new verbs on its own. Reaching agents needs either a
// gateway-side plugin that registers the command, or routing through a command that is
// already allowlisted. They are kept here because the handlers work and only the
// registration path is missing.
func pluginTools() []map[string]any {
	return []map[string]any{
		{
			"pluginId":    "clawhq",
			"name":        "clawhq_departments_list",
			"description": "List ClawHQ departments and which agents belong to each.",
			"command":     CmdDepartmentsList,
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"pluginId":    "clawhq",
			"name":        "clawhq_department_create",
			"description": "Create a ClawHQ department (or update one that already exists).",
			"command":     CmdDepartmentCreate,
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":  map[string]any{"type": "string", "description": "Display name, e.g. Research"},
					"emoji": map[string]any{"type": "string", "description": "Optional emoji for the sidebar"},
				},
				"required": []string{"name"},
			},
		},
		{
			"pluginId": "clawhq",
			"name":     "clawhq_ask_human",
			"description": "Ask the person at this machine for help, e.g. to log in to a site, approve " +
				"something, or answer a question. Shows a notification on their screen that names you. " +
				"Always pass your own agent id.",
			"command": CmdSystemNotify,
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":   map[string]any{"type": "string", "description": "Short headline, e.g. Login needed"},
					"body":    map[string]any{"type": "string", "description": "What you need the person to do"},
					"agentId": map[string]any{"type": "string", "description": "Your agent id, e.g. main"},
				},
				"required": []string{"body", "agentId"},
			},
		},
		{
			"pluginId":    "clawhq",
			"name":        "clawhq_clipboard_get",
			"description": "Read the text on this machine's clipboard. Needs desktop control switched on in ClawHQ.",
			"command":     CmdClipboardGet,
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"pluginId":    "clawhq",
			"name":        "clawhq_clipboard_set",
			"description": "Put text on this machine's clipboard, e.g. to paste it into an app with computer.act. Needs desktop control switched on in ClawHQ.",
			"command":     CmdClipboardSet,
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string", "description": "Text to place on the clipboard"},
				},
				"required": []string{"text"},
			},
		},
		{
			"pluginId":    "clawhq",
			"name":        "clawhq_agent_assign",
			"description": "Put an agent into a ClawHQ department. An empty departmentId unassigns it.",
			"command":     CmdAgentAssign,
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agentId":      map[string]any{"type": "string", "description": "Agent id, e.g. marketing"},
					"departmentId": map[string]any{"type": "string", "description": "Department id from clawhq_departments_list"},
				},
				"required": []string{"agentId"},
			},
		},
	}
}

// SetHiddenTools withholds some of ClawHQ's tools from the gateway, for when the
// ClawHQ gateway plugin provides the same ones with the caller's real identity. The
// change is published straight away when the node is connected.
func (h *Host) SetHiddenTools(ctx context.Context, names []string) {
	hidden := map[string]bool{}
	for _, n := range names {
		hidden[n] = true
	}
	h.mu.Lock()
	h.hiddenTools = hidden
	h.mu.Unlock()
	h.publishPluginTools(ctx)
}

// publishPluginTools advertises ClawHQ's tools to the gateway so agents can call them.
func (h *Host) publishPluginTools(ctx context.Context) {
	h.mu.RLock()
	client := h.client
	h.mu.RUnlock()
	if client == nil {
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	h.mu.RLock()
	hidden := h.hiddenTools
	h.mu.RUnlock()
	tools := make([]map[string]any, 0)
	for _, t := range pluginTools() {
		if name, _ := t["name"].(string); hidden[name] {
			continue
		}
		tools = append(tools, t)
	}
	resp, err := client.Send(reqCtx, "node.pluginTools.update", map[string]any{
		"tools": tools,
	})
	if err != nil {
		if h.logUnknown != nil {
			h.logUnknown("node.pluginTools.update failed", err.Error())
		}
		return
	}
	if !resp.OK && resp.Error != nil {
		if h.logUnknown != nil {
			h.logUnknown("node.pluginTools.update rejected", resp.Error.Message)
		}
		return
	}
	if h.logUnknown != nil {
		h.logUnknown("node.pluginTools.update ok", string(resp.Payload))
	}
}
