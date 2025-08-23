package plex

import (
	"bytes"
	"testing"
	"time"

	"github.com/go-kit/log"
	plex "github.com/jrudio/go-plex-client"
)

// fakePlex implements minimal methods used by plexListener
type fakePlex struct{}

func (f *fakePlex) GetSessions() (plex.CurrentSessions, error) {
	s := plex.CurrentSessions{}
	s.MediaContainer.Metadata = []plex.Metadata{{
		SessionKey: "sess1",
		User:       plex.User{Title: "user1", ID: "u1"},
	}, {
		SessionKey: "sess2",
		User:       plex.User{Title: "user2", ID: "u2"},
	}}
	return s, nil
}

func (f *fakePlex) GetMetadata(ratingKey string) (plex.MediaMetadata, error) {
	mm := plex.MediaMetadata{}
	mm.MediaContainer.Metadata = []plex.Metadata{{
		RatingKey: ratingKey,
		Title:     "Episode 1",
	}}
	return mm, nil
}

// implement the methods used by the real plex.Plex interface used here
// Note: We only implement the two methods GetSessions and GetMetadata used
// by onPlaying.

func TestOnPlayingUpdatesSessions(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            log.NewNopLogger(),
	}

	// craft a notification container with one playing notification
	c := plex.NotificationContainer{}
	c.PlaySessionStateNotification = []plex.PlaySessionStateNotification{{
		SessionKey: "sess1",
		RatingKey:  "rk1",
		State:      "playing",
		ViewOffset: 1000,
	}}

	if err := l.onPlaying(c); err != nil {
		t.Fatalf("onPlaying error: %v", err)
	}

	// ensure session was added
	if _, ok := l.activeSessions.sessions["sess1"]; !ok {
		t.Fatalf("expected session sess1 to be present")
	}

	// ensure metadata was attached
	ss := l.activeSessions.sessions["sess1"]
	if ss.media.RatingKey != "rk1" && ss.session.SessionKey != "sess1" {
		t.Fatalf("unexpected session contents: %+v", ss)
	}

	// simulate stop + old timestamp and prune
	ss.state = stateStopped
	ss.lastUpdate = time.Now().Add(-2 * sessionTimeout)
	l.activeSessions.sessions["sess1"] = ss
	l.activeSessions.pruneOldSessions()
	if _, ok := l.activeSessions.sessions["sess1"]; ok {
		t.Fatalf("expected session sess1 to be pruned")
	}
}

func TestOnPlayingLogsFirstNotification(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	// capture logs in a buffer
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            logger,
	}

	// craft a notification container with two playing notifications
	c := plex.NotificationContainer{}
	c.PlaySessionStateNotification = []plex.PlaySessionStateNotification{{
		SessionKey: "sess1",
		RatingKey:  "rk1",
		State:      "playing",
		ViewOffset: 1000,
	}, {
		SessionKey: "sess2",
		RatingKey:  "rk2",
		State:      "playing",
		ViewOffset: 2000,
	}}

	if err := l.onPlaying(c); err != nil {
		t.Fatalf("onPlaying error: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Fatalf("expected log output, got empty string")
	}

	// Ensure the first session key and batchCount=2 are present in the log
	if !bytes.Contains([]byte(out), []byte("SessionKey=sess1")) {
		t.Fatalf("expected SessionKey=sess1 in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("batchCount=2")) {
		t.Fatalf("expected batchCount=2 in log, got: %s", out)
	}
}

func TestOnTimelineLogsEntries(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            logger,
	}

	// craft a notification container with two timeline entries
	c := plex.NotificationContainer{}
	c.TimelineEntry = []plex.TimelineEntry{{
		Identifier: "id1",
		ItemID:     123,
		Title:      "Test1",
		SectionID:  15,
		State:      2,
	}, {
		Identifier: "id2",
		ItemID:     124,
		Title:      "Test2",
		SectionID:  15,
		State:      3,
	}}

	// our handler expects the vendored NotificationContainer type used by
	// the websocket client; convert and call the handler.
	l.onTimelineHandler(c)

	out := buf.String()
	if out == "" {
		t.Fatalf("expected log output, got empty string")
	}

	if !bytes.Contains([]byte(out), []byte("timeline entries")) {
		t.Fatalf("expected timeline entries in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("id=id1")) {
		t.Fatalf("expected id=id1 in log, got: %s", out)
	}
}
