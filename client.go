package odoo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls the Odoo JSON-2 API. Configure its fields before making calls;
// a configured Client can be shared by concurrent callers.
type Client struct {
	BaseURL string
	APIKey  string
	DB      string

	// HTTP may be replaced to customize transport or timeout. When nil, a
	// client with a 30-second timeout is used.
	HTTP *http.Client

	// Retry controls retries of read-only calls (CallRead and the Search*/Get*
	// methods). The zero value disables retries. Call and every write helper (Create*, Update*,
	// UpsertPartnerByRef, SetProductActive, ConfirmSaleOrder, CancelSaleOrder) never retry.
	Retry RetryPolicy
}

// NewClient constructs a client for an Odoo server URL (without /json/2).
// An empty db omits the X-Odoo-Database header.
func NewClient(baseURL, apiKey, db string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		DB:      db,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Call invokes a model method with named JSON parameters. Nil parameters send
// an empty object. A non-nil result must be a pointer suitable for json decoding;
// a nil result discards the successful response. Calls are never retried, since
// model methods may have side effects.
func (c *Client) Call(
	ctx context.Context,
	model, method string,
	params any,
	result any,
) error {
	return c.call(ctx, model, method, params, result)
}

// call performs exactly one request attempt.
func (c *Client) call(
	ctx context.Context,
	model, method string,
	params any,
	result any,
) error {
	var (
		err        error
		body       []byte
		endpoint   string
		req        *http.Request
		httpClient *http.Client
		resp       *http.Response
		msg        []byte
	)

	if body, err = json.Marshal(params); err != nil {
		return fmt.Errorf("encode parameters: %w", err)
	}
	body = bytes.TrimSpace(body)
	if bytes.Equal(body, []byte("null")) {
		body = []byte("{}")
	}
	if len(body) == 0 || body[0] != '{' {
		return invalidArgument("parameters must encode to a JSON object")
	}

	endpoint = strings.TrimRight(c.BaseURL, "/") + "/json/2/" +
		url.PathEscape(model) + "/" + url.PathEscape(method)

	req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if c.DB != "" {
		req.Header.Set("X-Odoo-Database", c.DB)
	}

	httpClient = c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err = httpClient.Do(req)
	if err != nil {
		return &transportError{Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, err = io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			return fmt.Errorf("HTTP %d: read error response: %w", resp.StatusCode, err)
		}
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(msg),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		}
	}

	if result == nil {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("%w: decode response: %w", ErrUnexpectedResponse, err)
	}
	return nil
}

// CallRead is like Call but retries transient failures according to c.Retry. Use it only for
// methods without side effects (search_read, read, and similar). Retries stop on success, on a
// non-transient error, when attempts are exhausted, or when ctx is done.
func (c *Client) CallRead(
	ctx context.Context,
	model, method string,
	params any,
	result any,
) error {
	var (
		attempt int
		err     error
	)

	for attempt = 0; ; attempt++ {
		err = c.call(ctx, model, method, params, result)
		if err == nil || attempt >= c.Retry.MaxRetries || ctx.Err() != nil || !retryable(err) {
			return err
		}
		if waitErr := sleepContext(ctx, c.Retry.wait(attempt, err)); waitErr != nil {
			return err
		}
	}
}
