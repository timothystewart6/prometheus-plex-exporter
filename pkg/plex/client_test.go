package plex

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"
)

type simpleRT struct {
	status int
	body   string
}

func (s *simpleRT) RoundTrip(req *http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode: s.status,
		Status:     "OK",
		Body:       io.NopCloser(bytes.NewBufferString(s.body)),
		Header:     make(http.Header),
		Request:    req,
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

func TestNewRequestHeaders(t *testing.T) {
	c, err := NewClient("http://example.com", "tok123")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}
	req, err := c.NewRequest("GET", "/path")
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if req.Header.Get("X-Plex-Token") != "tok123" {
		t.Fatalf("expected token header set")
	}
}

func TestDo404AndDecode(t *testing.T) {
	c, _ := NewClient("http://example.com", "tok")
	// test 404 handling
	c.httpClient = http.Client{Transport: &simpleRT{status: 404, body: "{}"}, Timeout: time.Second}
	_, err := c.NewRequest("GET", "/notfound")
	if err != nil {
		t.Fatalf("unexpected NewRequest err: %v", err)
	}
	// Build a request and call Do directly
	req, _ := c.NewRequest("GET", "/notfound")
	err = c.Do(req, &struct{}{})
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// test decode
	c.httpClient = http.Client{Transport: &simpleRT{status: 200, body: `{"foo":"bar"}`}, Timeout: time.Second}
	req, _ = c.NewRequest("GET", "/ok")
	var out map[string]string
	if err := c.Do(req, &out); err != nil {
		t.Fatalf("expected decode success, got %v", err)
	}
	if out["foo"] != "bar" {
		t.Fatalf("unexpected decode result: %v", out)
	}
}
