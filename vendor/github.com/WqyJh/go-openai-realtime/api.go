package openairt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type CreateSessionRequest struct {
	ClientSession

	// The Realtime model used for this session.
	Model string `json:"model"`
}

type ClientSecret struct {
	// Ephemeral key usable in client environments to authenticate connections to the Realtime API. Use this in client-side environments rather than a standard API token, which should only be used server-side.
	Value string `json:"value"`
	// Timestamp for when the token expires. Currently, all tokens expire after one minute.
	ExpiresAt int64 `json:"expires_at"`
}

type CreateSessionResponse struct {
	ServerSession

	// Ephemeral key returned by the API.
	ClientSecret ClientSecret `json:"client_secret"`
}

type httpOption struct {
	client  *http.Client
	headers http.Header
	method  string
}

type HTTPOption func(*httpOption)

func WithHeaders(headers http.Header) HTTPOption {
	return func(o *httpOption) {
		o.headers = headers
	}
}

func WithClient(client *http.Client) HTTPOption {
	return func(o *httpOption) {
		o.client = client
	}
}

func WithMethod(method string) HTTPOption {
	return func(o *httpOption) {
		o.method = method
	}
}

func HTTPDo[Q any, R any](ctx context.Context, url string, req *Q, opts ...HTTPOption) (*R, error) {
	opt := httpOption{
		client:  http.DefaultClient,
		headers: http.Header{},
		method:  http.MethodPost,
	}
	for _, o := range opts {
		o(&opt)
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, opt.method, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	request.Header = opt.headers

	response, err := opt.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("http failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status code: %d", response.StatusCode)
	}

	var resp R
	err = json.NewDecoder(response.Body).Decode(&resp)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}
