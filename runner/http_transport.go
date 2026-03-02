package runner

import (
	"context"
	"io"
	"net/http"

	"github.com/lamoda/gonkey/models"
)

// Compile-time interface check (ES-0053).
var _ TransportExecutor = (*HttpTransport)(nil)

type HttpTransport struct {
	cfg    *Config
	client *http.Client
}

func newHttpTransport(cfg *Config) *HttpTransport { //nolint:unused // called from newTransportExecutor, will be wired in a later task
	return &HttpTransport{
		cfg:    cfg,
		client: newClient(cfg.HTTPProxyURL), // newClient остаётся в request.go
	}
}

func (t *HttpTransport) Execute(ctx context.Context, test models.TestInterface) (*models.Result, error) {
	req, err := newRequest(t.cfg.Host, test)
	if err != nil {
		return nil, err
	}

	resp, err := t.client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)

	_ = resp.Body.Close()

	if err != nil {
		return nil, err
	}

	bodyStr := string(body)

	return &models.Result{
		Path:                req.URL.Path,
		Query:               req.URL.RawQuery,
		RequestBody:         actualRequestBody(req),
		ResponseBody:        bodyStr,
		ResponseContentType: resp.Header.Get("Content-Type"),
		ResponseStatusCode:  resp.StatusCode,
		ResponseStatus:      resp.Status,
		ResponseHeaders:     resp.Header,
		Test:                test,
	}, nil
}
