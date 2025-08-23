package plex

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	ttPlex "github.com/timothystewart6/go-plex-client"
)

func TestSubscribeToNotifications_InvokesEventHandler(t *testing.T) {
	// Start a test server that upgrades websocket connections and sends a single notification
	serverDone := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/:/websockets/notifications" {
			http.NotFound(w, r)
			return
		}
		up := websocket.Upgrader{}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer c.Close()

		// send a transcodeSession.update notification (NotificationContainer.type must be set)
		msg := `{"NotificationContainer":{"TranscodeSession":[{"key":"k1","videoDecision":"transcode","subtitleDecision":"copy","container":"mp4"}],"type":"transcodeSession.update"}}`
		if err := c.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			t.Fatalf("write message: %v", err)
		}

		// keep the connection open until the test signals serverDone
		<-serverDone
	}))
	defer srv.Close()

	// Create a Plex client pointing at the test server
	p, err := ttPlex.New(srv.URL, "token")
	if err != nil {
		t.Fatalf("failed to create plex client: %v", err)
	}

	events := ttPlex.NewNotificationEvents()
	called := make(chan struct{}, 1)
	events.OnTranscodeUpdate(func(n ttPlex.NotificationContainer) {
		called <- struct{}{}
	})

	// interrupt channel: close after timeout to allow SubscribeToNotifications to exit
	intr := make(chan os.Signal, 1)

	var gotErr error
	done := make(chan struct{})
	var doneOnce sync.Once
	go func() {
		p.SubscribeToNotifications(events, intr, func(e error) {
			gotErr = e
			doneOnce.Do(func() { close(done) })
		})
	}()

	select {
	case <-called:
		// success
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout waiting for event handler to be called")
	}

	// close interrupt to make SubscribeToNotifications return
	close(intr)

	// allow server handler to return (which closes the server-side websocket)
	close(serverDone)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for SubscribeToNotifications to finish")
	}

	if gotErr != nil {
		t.Logf("SubscribeToNotifications reported error (acceptable): %v", gotErr)
	}
}
