package output

import (
	"github.com/Issengaard/gonkey_grpc/models"
)

type OutputInterface interface {
	Process(models.TestInterface, *models.Result) error
}
