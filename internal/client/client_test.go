package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreatedID(t *testing.T) {
	cases := map[string]int64{
		`{"data":{"id":216198},"message":"Backend set registered successfully.","status":0}`: 216198,
		`{"id":7}`:            7,
		`[{"id":8}]`:          8,
		`{"data":[{"id":9}]}`: 9,
		`"10"`:                10,
		`11`:                  11,
	}
	for in, want := range cases {
		got, ok := CreatedID(json.RawMessage(in))
		if !ok || got != want {
			t.Errorf("CreatedID(%s) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{``, `{"status":0}`, `[]`, `[{"id":1},{"id":2}]`, `{"id":0}`} {
		if got, ok := CreatedID(json.RawMessage(in)); ok {
			t.Errorf("CreatedID(%s) = %d, want no id", in, got)
		}
	}
}

func TestDecodeOne(t *testing.T) {
	var v struct{ ID int64 }
	if err := DecodeOne(json.RawMessage(`[{"id":3}]`), &v); err != nil || v.ID != 3 {
		t.Fatalf("array: %v %+v", err, v)
	}
	if err := DecodeOne(json.RawMessage(`{"id":4}`), &v); err != nil || v.ID != 4 {
		t.Fatalf("object: %v %+v", err, v)
	}
	if err := DecodeOne(json.RawMessage(`[]`), &v); !NotFound(err) {
		t.Fatalf("empty array should be NotFound, got %v", err)
	}
}

func TestErrorMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" {
			t.Errorf("missing X-API-Key header")
		}
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"Network not found"}`))
		case "/invalid":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"UpdateDomainRequest.Name":"Name is a required field","UpdateDomainRequest.BAC":"BAC is a required field"}}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "k", "test")

	err := c.Get(context.Background(), "/missing", nil)
	if !NotFound(err) || err.(*APIError).Message != "Network not found" {
		t.Fatalf("want NotFound with message, got %v", err)
	}
	if err := c.Delete(context.Background(), "/missing"); err != nil {
		t.Fatalf("Delete of a missing entity should succeed, got %v", err)
	}
	_, err = c.Write(context.Background(), http.MethodPatch, "/invalid", map[string]any{}, nil)
	want := "UpdateDomainRequest.BAC: BAC is a required field; UpdateDomainRequest.Name: Name is a required field"
	if err == nil || err.(*APIError).Message != want {
		t.Fatalf("got %v, want message %q", err, want)
	}
}

func TestRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		switch {
		case r.URL.Path == "/limited" && n == 1:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		case r.URL.Path == "/flaky" && n == 1:
			w.WriteHeader(http.StatusBadGateway)
		case r.URL.Path == "/write":
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "k", "test")
	c.backoff = time.Millisecond

	for _, p := range []string{"/limited", "/flaky"} {
		calls.Store(0)
		if err := c.Get(context.Background(), p, nil); err != nil || calls.Load() != 2 {
			t.Fatalf("%s: err=%v calls=%d, want success on the 2nd call", p, err, calls.Load())
		}
	}
	// A 5xx on a write may have landed; it must not be repeated.
	calls.Store(0)
	if _, err := c.Write(context.Background(), http.MethodPost, "/write", map[string]any{}, nil); err == nil || calls.Load() != 1 {
		t.Fatalf("write: err=%v calls=%d, want one failed call", err, calls.Load())
	}
}
