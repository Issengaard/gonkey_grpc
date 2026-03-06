package testloader

import (
	"github.com/Issengaard/gonkey_grpc/models"
)

type LoaderInterface interface {
	Load() ([]models.TestInterface, error)
}
