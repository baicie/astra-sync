// Package compilerclient owns the bounded internal Java compiler RPC boundary.
package compilerclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"

	compilerv1 "io.astrasync/control-plane/api-server/gen/go/compiler/v1"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

const maximumMessageBytes = 1024 * 1024

type Client struct {
	client  compilerv1.CompilerValidationServiceClient
	timeout time.Duration
}

func New(connection grpc.ClientConnInterface, timeout time.Duration) (*Client, error) {
	if connection == nil || timeout <= 0 || timeout > 30*time.Second {
		return nil, fmt.Errorf("compiler client connection and bounded timeout are required")
	}
	return &Client{client: compilerv1.NewCompilerValidationServiceClient(connection), timeout: timeout}, nil
}

func (c *Client) Validate(
	ctx context.Context, request *compilerv1.ValidateRequest,
) (*compilerv1.ValidateResponse, error) {
	callContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.client.Validate(
		callContext, request,
		grpc.MaxCallSendMsgSize(maximumMessageBytes), grpc.MaxCallRecvMsgSize(maximumMessageBytes),
	)
}

func (c *Client) Inventory(
	ctx context.Context, executionProfile string,
) (*controlv1.ConnectorInventory, error) {
	callContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.client.GetInventory(
		callContext,
		&compilerv1.GetInventoryRequest{ExecutionProfile: executionProfile},
		grpc.MaxCallSendMsgSize(maximumMessageBytes), grpc.MaxCallRecvMsgSize(maximumMessageBytes),
	)
	if err != nil {
		return nil, err
	}
	if response.GetInventory() == nil {
		return nil, fmt.Errorf("compiler inventory response is empty")
	}
	return response.GetInventory(), nil
}
