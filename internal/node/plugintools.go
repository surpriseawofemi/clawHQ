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

	resp, err := client.Send(reqCtx, "node.pluginTools.update", map[string]any{
		"tools": pluginTools(),
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
