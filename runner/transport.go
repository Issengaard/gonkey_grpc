package runner

import (
	"context"
	"fmt"
	"os"

	"github.com/Issengaard/gonkey_grpc/models"
)

// transportExecutor executes the transport layer for a single test case.
type transportExecutor interface {
	Execute(ctx context.Context, test models.TestInterface) (*models.Result, error)
}

// newTransportExecutor dispatches to the appropriate transport based on the
// test's transport field. Unknown transport values return an error (no panic).
func newTransportExecutor(test models.TestInterface, cfg *Config) (transportExecutor, error) {
	switch test.GetTransport() {
	case "":
		fmt.Fprintf(os.Stderr, "WARN: transport field is empty, defaulting to HTTP. Set transport: http explicitly.\n")

		return newHttpTransport(cfg), nil
	case "http":
		return newHttpTransport(cfg), nil
	case "grpc":
		return newGrpcTransport(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported transport: %s", test.GetTransport())
	}
}
