// Package client is a minimal client for the TCPShield REST API.
//
// Shape verified against the live API (2026-10):
//   - Base URL: https://api.tcpshield.com ; auth via the X-API-Key header.
//   - Plain JSON, no envelope: a collection is a bare array, a single entity a
//     bare object. /user/summary is the exception and wraps in {"data": …}.
//   - Errors are {"error": "<message>"} with a meaningful status code, 404 for a
//     missing entity included ("Network not found", "Domain not found", …).
//   - The published OpenAPI spec (swagger.tcpshield.com) is loose about response
//     shapes — several single-entity reads are documented as arrays and most
//     writes as "200 OK" with no body — so callers decode defensively through
//     DecodeOne and CreatedID rather than trusting it.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultEndpoint is the base URL of the TCPShield API.
const DefaultEndpoint = "https://api.tcpshield.com"

// maxAttempts bounds the retry loop for 429 and transient 5xx responses.
const maxAttempts = 5

// Client talks to the TCPShield API.
type Client struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
	userAgent  string
	// backoff is the base delay between retries; tests shrink it.
	backoff time.Duration
}

// New constructs a Client. An empty endpoint falls back to DefaultEndpoint.
func New(endpoint, apiKey, userAgent string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		endpoint:   strings.TrimRight(endpoint, "/"),
		apiKey:     apiKey,
		userAgent:  userAgent,
		backoff:    time.Second,
	}
}

// Get fetches path and decodes the JSON body into out (when non-nil).
func (c *Client) Get(ctx context.Context, path string, out any) error {
	raw, err := c.Do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return decode(raw, out, http.MethodGet, path)
}

// Write sends body as JSON with method and decodes the response into out (when
// non-nil and the response is not empty). It returns the raw body as well,
// because several TCPShield writes answer with an id the caller has to find.
func (c *Client) Write(ctx context.Context, method, path string, body, out any) (json.RawMessage, error) {
	raw, err := c.Do(ctx, method, path, body)
	if err != nil {
		return raw, err
	}
	return raw, decode(raw, out, method, path)
}

// Delete issues a DELETE against path. A 404 counts as success: the entity is
// gone either way, which is what a destroy asks for.
func (c *Client) Delete(ctx context.Context, path string) error {
	_, err := c.Do(ctx, http.MethodDelete, path, nil)
	if NotFound(err) {
		return nil
	}
	return err
}

func decode(raw json.RawMessage, out any, method, path string) error {
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding %s %s response: %w", method, path, err)
	}
	return nil
}

// DecodeOne decodes a single entity that the API returns either as a bare object
// or wrapped in a one-element array (the spec documents several reads that way).
func DecodeOne(raw json.RawMessage, out any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var list []json.RawMessage
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			return &APIError{StatusCode: http.StatusNotFound, Message: "empty response"}
		}
		trimmed = list[0]
	}
	return json.Unmarshal(trimmed, out)
}

// CreatedID extracts the id of a freshly created entity from a write response,
// tolerating every shape seen in the wild: {"id": n}, {"data": {"id": n}},
// [{"id": n}], a bare number, and ids encoded as strings. ok is false when the
// response carries no id at all — the caller then has to look the entity up.
func CreatedID(raw json.RawMessage) (int64, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 0, false
	}
	var v any
	if err := json.Unmarshal(trimmed, &v); err != nil {
		return 0, false
	}
	return findID(v)
}

func findID(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		if t > 0 && t == math.Trunc(t) {
			return int64(t), true
		}
	case string:
		if n, err := strconv.ParseInt(t, 10, 64); err == nil && n > 0 {
			return n, true
		}
	case []any:
		if len(t) == 1 {
			return findID(t[0])
		}
	case map[string]any:
		if id, ok := t["id"]; ok {
			return findID(id)
		}
		if data, ok := t["data"]; ok {
			return findID(data)
		}
	}
	return 0, false
}

// NotFound reports whether err is a 404 from the API, so Read can drop an
// entity that was deleted out of band.
func NotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == http.StatusNotFound
}

// APIError is a non-2xx API response.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	// Message is the API's {"error": …} text, or the raw body when it has none.
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tcpshield API %s %s: status %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
}

// Do performs an authenticated request with body JSON-encoded (when non-nil) and
// returns the raw 2xx response body. 429 and 5xx are retried with backoff,
// honouring Retry-After.
func (c *Client) Do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = b
	}

	for attempt := 1; ; attempt++ {
		raw, retryAfter, err := c.once(ctx, method, path, payload)
		if err == nil || retryAfter < 0 || attempt == maxAttempts {
			return raw, err
		}
		if retryAfter == 0 {
			retryAfter = c.backoff * time.Duration(1<<(attempt-1))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryAfter):
		}
	}
}

// once sends a single request. retryAfter is negative when the failure is not
// retryable, zero for "retry with the default backoff".
func (c *Client) once(ctx context.Context, method, path string, payload []byte) (json.RawMessage, time.Duration, error) {
	var r io.Reader
	if payload != nil {
		r = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, r)
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		// A transport error on a read is safe to retry; on a write it may have
		// landed, and repeating a create would duplicate it.
		if method == http.MethodGet {
			return nil, 0, err
		}
		return nil, -1, err
	}
	defer func() { _ = res.Body.Close() }()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, -1, err
	}

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return data, -1, nil
	}

	apiErr := &APIError{StatusCode: res.StatusCode, Method: method, Path: path, Message: errorMessage(data)}
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, retryAfterHeader(res.Header.Get("Retry-After")), apiErr
	case res.StatusCode >= 500 && method == http.MethodGet:
		return nil, 0, apiErr
	default:
		return nil, -1, apiErr
	}
}

// errorMessage extracts the API's error text. "error" is a string for most
// failures but an object of field → message for validation errors
// ({"UpdateDomainRequest.Name": "Name is a required field"}); those render as
// "field: message" pairs, sorted so the text is stable.
func errorMessage(body []byte) string {
	var e struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil {
		var s string
		if json.Unmarshal(e.Error, &s) == nil && s != "" {
			return s
		}
		var fields map[string]string
		if json.Unmarshal(e.Error, &fields) == nil && len(fields) > 0 {
			parts := make([]string, 0, len(fields))
			for k, v := range fields {
				parts = append(parts, k+": "+v)
			}
			sort.Strings(parts)
			return strings.Join(parts, "; ")
		}
		if e.Message != "" {
			return e.Message
		}
	}
	return strings.TrimSpace(string(body))
}

func retryAfterHeader(v string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}
