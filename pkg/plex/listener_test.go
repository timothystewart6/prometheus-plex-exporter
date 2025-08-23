package plex

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	plexv "github.com/jrudio/go-plex-client"
)

// fakePlex implements minimal methods used by plexListener
type fakePlex struct{}

func (f *fakePlex) GetSessions() (plexv.CurrentSessions, error) {
	s := plexv.CurrentSessions{}
	s.MediaContainer.Metadata = []plexv.Metadata{{
		SessionKey: "sess1",
		User:       plexv.User{Title: "user1", ID: "u1"},
	}}
	return s, nil
}

func (f *fakePlex) GetMetadata(ratingKey string) (plexv.MediaMetadata, error) {
	mm := plexv.MediaMetadata{}
	mm.MediaContainer.Metadata = []plexv.Metadata{{
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
	c := plexv.NotificationContainer{}
	c.PlaySessionStateNotification = []plexv.PlaySessionStateNotification{{
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
