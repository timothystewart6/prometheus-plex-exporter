package plex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gorilla/websocket"
	ttPlex "github.com/timothystewart6/go-plex-client"
)

// TestListen_SuccessfulSubscribePath verifies that Server.Listen returns when
// the websocket notifications connection closes normally. It overrides
// newPlex to construct a client pointed at an httptest websocket server which
// immediately sends a normal close frame.
func TestListen_SuccessfulSubscribePath(t *testing.T) {
	// httptest server that upgrades to websocket and immediately closes
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var upgrader websocket.Upgrader
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		// Send a normal close so the client's read loop returns a CloseError
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = conn.Close()
	}))
	defer ts.Close()

	// override newPlex to return a client that points at our test server
	old := newPlex
	newPlex = func(base, token string) (*ttPlex.Plex, error) {
		return ttPlex.New(base, token)
	}
	defer func() { newPlex = old }()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	s := &Server{URL: u, Token: "tok"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Expect Listen to return (nil on normal closure)
	if err := s.Listen(ctx, log.NewNopLogger()); err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
}
