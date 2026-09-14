package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/a3tai/openclaw-go/protocol"
)

// sendRPC sends a typed request and returns the raw response payload.
// It returns an error if the response is not OK.
func (c *Client) sendRPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	resp, err := c.Send(ctx, method, params)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, rpcError(method, resp)
	}
	return resp.Payload, nil
}

// sendRPCTyped sends a typed request and unmarshals the response into result.
func (c *Client) sendRPCTyped(ctx context.Context, method string, params any, result any) error {
	payload, err := c.sendRPC(ctx, method, params)
	if err != nil {
		return err
	}
	if result != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, result); err != nil {
			return fmt.Errorf("%s: unmarshal: %w", method, err)
		}
	}
	return nil
}

// sendRPCVoid sends a typed request and discards the response payload.
func (c *Client) sendRPCVoid(ctx context.Context, method string, params any) error {
	_, err := c.sendRPC(ctx, method, params)
	return err
}

// rpcError creates an error from a failed RPC response.
func rpcError(method string, resp *protocol.Response) error {
	if resp.Error != nil {
		return fmt.Errorf("%s: %s: %s", method, resp.Error.Code, resp.Error.Message)
	}
	return fmt.Errorf("%s: request failed", method)
}
