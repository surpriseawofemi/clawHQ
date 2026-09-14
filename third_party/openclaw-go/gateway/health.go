package gateway

import (
	"context"
	"encoding/json"
)

// Health retrieves the gateway health status.
func (c *Client) Health(ctx context.Context) (json.RawMessage, error) {
	return c.sendRPC(ctx, "health", nil)
}

// Status retrieves the gateway status.
func (c *Client) Status(ctx context.Context) (json.RawMessage, error) {
	return c.sendRPC(ctx, "status", nil)
}
