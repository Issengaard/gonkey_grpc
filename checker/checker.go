package checker

import "github.com/Issengaard/gonkey_grpc/models"

type CheckerInterface interface {
	Check(models.TestInterface, *models.Result) ([]error, error)
}
