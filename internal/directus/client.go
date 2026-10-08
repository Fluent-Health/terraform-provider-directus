// Package directus is a small hand-written HTTP client for the Directus REST
// API, scoped to the operations this Terraform provider needs.
//
// It is deliberately not generated from the server's /server/specs/oas
// document: that spec is rendered from the live schema (every user collection
// shows up as its own paths), so it differs per instance and per migration.
package directus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultTimeout caps one request, including the retry transport's waits.
const defaultTimeout = 2 * time.Minute

// Client talks to one Directus instance with a static access token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	cache   *listCache
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client (tests). The caller owns its
// transport, so no retry layer is added.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// NewClient builds a Client for the Directus instance at baseURL.
func NewClient(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout:   defaultTimeout,
			Transport: newRetryTransport(http.DefaultTransport),
		},
		cache: newListCache(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ErrNotFound is returned by Get* methods when the object does not exist.
//
// Directus never answers 404 for a missing item: ItemsService.readOne throws
// ForbiddenError, so a missing row and a missing permission are the same 403.
// The client therefore decides "not found" by listing the collection (which an
// authorised token can always do) and looking the key up; a 403 on that list is
// a real permission error and is returned as such.
var ErrNotFound = errors.New("directus: object not found")

// goneOn403 resolves a 403 from a write against a missing object (Directus'
// "not found") into nil when a fresh listing confirms the object is absent, and
// otherwise returns err unchanged: a 403 for an object that IS listed is a real
// permission error. exists must read through the (just invalidated) cache.
func goneOn403(ctx context.Context, err error, exists func(context.Context) error) error {
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != http.StatusForbidden {
		return err
	}
	if lookup := exists(ctx); IsNotFound(lookup) {
		return nil
	}
	return err
}

// IsNotFound reports whether err means the object does not exist.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// APIError is returned for any non-2xx response.
type APIError struct {
	StatusCode int
	// Code is the first error's extensions.code, e.g. FORBIDDEN,
	// INVALID_PAYLOAD, RECORD_NOT_UNIQUE.
	Code    string
	Message string
}

func (e *APIError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("directus API error: HTTP %d %s: %s", e.StatusCode, e.Code, e.Message)
	case e.Message != "":
		return fmt.Sprintf("directus API error: HTTP %d: %s", e.StatusCode, e.Message)
	default:
		return fmt.Sprintf("directus API error: HTTP %d", e.StatusCode)
	}
}

// errorEnvelope is Directus' error body: {"errors":[{"message","extensions":{"code"}}]}.
type errorEnvelope struct {
	Errors []struct {
		Message    string `json:"message"`
		Extensions struct {
			Code string `json:"code"`
		} `json:"extensions"`
	} `json:"errors"`
}

// dataEnvelope is Directus' success body: {"data": ...}.
type dataEnvelope struct {
	Data json.RawMessage `json:"data"`
}

// do performs a request and unmarshals the response's "data" member into out
// (if non-nil). body is JSON-marshaled unless nil.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(data))}
		var env errorEnvelope
		if json.Unmarshal(data, &env) == nil && len(env.Errors) > 0 {
			apiErr.Code = env.Errors[0].Extensions.Code
			apiErr.Message = env.Errors[0].Message
		}
		return apiErr
	}

	// 204 No Content (deletes) carries no body.
	if out == nil || len(data) == 0 {
		return nil
	}
	var env dataEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if raw, ok := out.(*json.RawMessage); ok {
		*raw = append((*raw)[:0], env.Data...)
		return nil
	}
	return json.Unmarshal(env.Data, out)
}
