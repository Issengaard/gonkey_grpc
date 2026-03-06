package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lamoda/gonkey/checker"
	"github.com/lamoda/gonkey/cmd_runner"
	"github.com/lamoda/gonkey/fixtures"
	"github.com/lamoda/gonkey/mocks"
	grpcmock "github.com/lamoda/gonkey/mocks/grpc"
	"github.com/lamoda/gonkey/models"
	"github.com/lamoda/gonkey/output"
	"github.com/lamoda/gonkey/testloader"
	"github.com/lamoda/gonkey/variables"
)

type Config struct {
	Host                  string
	GrpcHost              string
	FixturesLoader        fixtures.Loader
	FixturesLoaderMultiDb fixtures.LoaderMultiDb
	Mocks                 *mocks.Mocks
	MocksLoader           *mocks.Loader
	GrpcMocks             *grpcmock.GrpcMocks
	GrpcMocksLoader       *grpcmock.GrpcLoader
	Variables             *variables.Variables
	HTTPProxyURL          *url.URL
	RequestTimeout        time.Duration
}

type (
	testExecutor func(models.TestInterface) (*models.Result, error)
	testHandler  func(models.TestInterface, testExecutor) error
)

type Runner struct {
	loader               testloader.LoaderInterface
	testExecutionHandler testHandler
	output               []output.OutputInterface
	checkers             []checker.CheckerInterface
	config               *Config
	transports           map[string]transportExecutor
	mu                   sync.RWMutex // protects transports map
}

func New(config *Config, loader testloader.LoaderInterface, handler testHandler) *Runner {
	return &Runner{
		config:               config,
		loader:               loader,
		testExecutionHandler: handler,
		transports:           make(map[string]transportExecutor),
	}
}

func (r *Runner) AddOutput(o ...output.OutputInterface) {
	r.output = append(r.output, o...)
}

func (r *Runner) AddCheckers(c ...checker.CheckerInterface) {
	r.checkers = append(r.checkers, c...)
}

var (
	errTestSkipped = errors.New("test was skipped")
	errTestBroken  = errors.New("test was broken")
)

func (r *Runner) Run() error {
	tests, err := r.loader.Load()
	if err != nil {
		return err
	}

	defer func() {
		for _, t := range r.transports {
			if closer, ok := t.(io.Closer); ok {
				_ = closer.Close()
			}
		}
	}()

	hasFocused := checkHasFocused(tests)
	for _, t := range tests {
		// make a copy because go test runner runs tests in separate goroutines
		// and without copy tests will override each other
		test := t
		if hasFocused {
			switch test.GetStatus() {
			case "focus":
				test.SetStatus("")
			case "broken":
				// do nothing
			default:
				test.SetStatus("skipped")
			}
		}

		testExecutor := r.buildTestExecutor(test)
		err := r.testExecutionHandler(test, testExecutor)
		if err != nil {
			return fmt.Errorf("test %s error: %w", test.GetName(), err)
		}
	}

	return nil
}

func (r *Runner) buildTestExecutor(test models.TestInterface) testExecutor {
	return func(testInterface models.TestInterface) (*models.Result, error) {
		switch testInterface.GetStatus() {
		case "broken":
			return nil, errTestBroken
		case "skipped":
			return nil, errTestSkipped
		}

		testResult, err := r.executeTest(test)
		if err != nil {
			return nil, err
		}

		for _, o := range r.output {
			if err := o.Process(test, testResult); err != nil {
				return nil, err
			}
		}

		return testResult, nil
	}
}

func (r *Runner) getTransportExecutor(test models.TestInterface) (transportExecutor, error) {
	key := test.GetTransport()

	r.mu.RLock()
	ex, ok := r.transports[key]
	r.mu.RUnlock()

	if ok {
		return ex, nil
	}

	ex, err := newTransportExecutor(test, r.config)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.transports[key] = ex
	r.mu.Unlock()

	return ex, nil
}

func (r *Runner) loadFixtures(v models.TestInterface) error {
	if r.config.FixturesLoader != nil && v.Fixtures() != nil {
		if err := r.config.FixturesLoader.Load(v.Fixtures()); err != nil {
			return fmt.Errorf("unable to load fixtures [%s], error:\n%w", strings.Join(v.Fixtures(), ", "), err)
		}
	}

	if r.config.FixturesLoaderMultiDb != nil && v.FixturesMultiDb() != nil {
		if err := r.config.FixturesLoaderMultiDb.Load(v.FixturesMultiDb()); err != nil {
			return fmt.Errorf("unable to load fixtures with db, error:\n%w", err)
		}
	}

	return nil
}

func (r *Runner) setupMocks(v models.TestInterface) error {
	if r.config.Mocks != nil {
		// prevent deriving the definition from previous test
		r.config.Mocks.ResetDefinitions()
		r.config.Mocks.ResetRunningContext()
	}

	if r.config.MocksLoader != nil && v.ServiceMocks() != nil {
		if err := r.config.MocksLoader.Load(v.ServiceMocks()); err != nil {
			return err
		}
	}

	if r.config.GrpcMocks != nil {
		r.config.GrpcMocks.ResetAll()
		if grpcDefs := v.GrpcServiceMocks(); len(grpcDefs) > 0 && r.config.GrpcMocksLoader != nil {
			if err := r.config.GrpcMocksLoader.Load(grpcDefs); err != nil {
				return fmt.Errorf("grpc mocks: %w", err)
			}
		}
	}

	return nil
}

func (r *Runner) executeTest(v models.TestInterface) (*models.Result, error) {
	r.config.Variables.Load(v.GetCombinedVariables())
	v = r.config.Variables.Apply(v)

	if err := r.loadFixtures(v); err != nil {
		return nil, err
	}

	if err := r.setupMocks(v); err != nil {
		return nil, err
	}

	// Note: RequestTimeout applies only to transport.Execute().
	// Scripts use their own timeout via BeforeScriptTimeout/AfterRequestScriptTimeout.
	if v.BeforeScriptPath() != "" {
		if err := cmd_runner.CmdRun(v.BeforeScriptPath(), v.BeforeScriptTimeout()); err != nil {
			return nil, err
		}
	}

	// make pause
	pause := v.Pause()
	if pause > 0 {
		time.Sleep(time.Duration(pause) * time.Second)
		fmt.Printf("Sleep %ds before requests\n", pause)
	}

	executor, err := r.getTransportExecutor(v)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	if r.config.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.config.RequestTimeout)
		defer cancel()
	}

	result, err := executor.Execute(ctx, v)
	if err != nil {
		return nil, err
	}

	// launch script in cmd interface
	if v.AfterRequestScriptPath() != "" {
		if err := cmd_runner.CmdRun(v.AfterRequestScriptPath(), v.AfterRequestScriptTimeout()); err != nil {
			return nil, err
		}
	}

	if r.config.Mocks != nil {
		errs := r.config.Mocks.EndRunningContext()
		result.Errors = append(result.Errors, errs...)
	}

	if err := r.setVariablesFromResponse(v, result); err != nil {
		return nil, err
	}

	r.config.Variables.Load(v.GetCombinedVariables())
	v = r.config.Variables.Apply(v)

	for _, c := range r.checkers {
		errs, err := c.Check(v, result)
		if err != nil {
			return nil, err
		}
		result.Errors = append(result.Errors, errs...)
	}

	return result, nil
}

func (r *Runner) setVariablesFromResponse(t models.TestInterface, result *models.Result) error {
	varTemplates := t.GetVariablesToSet()
	if varTemplates == nil {
		return nil
	}

	var statusCode int
	var isJSON bool

	statusCode = result.ResponseStatusCode
	isJSON = strings.Contains(result.ResponseContentType, "json") && result.ResponseBody != ""

	if t.GetTransport() == "grpc" {
		statusCode = result.GrpcStatusCode
		isJSON = result.ResponseBody != ""
	}

	vars, err := variables.FromResponse(varTemplates[statusCode], result.ResponseBody, isJSON)
	if err != nil {
		return err
	}

	if vars == nil {
		return nil
	}

	r.config.Variables.Merge(vars)

	return nil
}

func checkHasFocused(tests []models.TestInterface) bool {
	for _, test := range tests {
		if test.GetStatus() == "focus" {
			return true
		}
	}

	return false
}
