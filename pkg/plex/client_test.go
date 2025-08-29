package plex

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ttPlex "github.com/timothystewart6/go-plex-client"
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
	c, err := NewClient("http://example.com", "fake-token-123")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}
	req, err := c.NewRequest("GET", "/path")
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	if req.Header.Get("X-Plex-Token") != "fake-token-123" {
		t.Fatalf("expected token header set")
	}
}

func TestDo404AndDecode(t *testing.T) {
	c, _ := NewClient("http://example.com", "fake-tok")
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

func TestInviteFriendEndToEnd(t *testing.T) {
	// httptest server to accept the invite request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/api/v2/shared_servers") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte("invitedEmail")) {
			t.Fatalf("request missing invitedEmail: %s", string(body))
		}

		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"id":"1","ownerId":"2","invitedId":"3","serverId":"4","numLibraries":"0","invited":{"id":"5"},"sharingSettings":{"allowTuners":"0"},"libraries":[]}`)); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	defer srv.Close()

	// create a client that points to the test server
	p, err := ttPlex.New(srv.URL, "fake-token")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	params := ttPlex.InviteFriendParams{UsernameOrEmail: "a@b.c", MachineID: "fake-machine", Label: "", LibraryIDs: []int{1}}
	if err := p.InviteFriend(params); err != nil {
		t.Fatalf("InviteFriend failed: %v", err)
	}
}
