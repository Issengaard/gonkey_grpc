package runner

import (
	"context"
	"fmt"
	"log"

	"github.com/lamoda/gonkey/models"
)

// TransportExecutor выполняет транспортный слой для одного тест-кейса.
type TransportExecutor interface {
	Execute(ctx context.Context, test models.TestInterface) (*models.Result, error)
}

// newTransportExecutor диспатчит на нужный транспорт по полю transport.
// Неизвестный транспорт → возвращает ошибку (не panic).
func newTransportExecutor(test models.TestInterface, cfg *Config) (TransportExecutor, error) { //nolint:unused // wired in later task
	switch test.GetTransport() {
	case "":
		log.Println("WARN: transport field is empty, defaulting to HTTP. Set transport: http explicitly.")

		return newHttpTransport(cfg), nil
	case "http":
		return newHttpTransport(cfg), nil
	case "grpc":
		return newGrpcTransport(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported transport: %s", test.GetTransport())
	}
}
