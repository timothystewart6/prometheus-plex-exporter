package plex

import (
	"testing"
	"time"

	"github.com/jrudio/go-plex-client"
)

func TestLabelsFunction(t *testing.T) {
	m := plex.Metadata{Type: "episode", GrandparentTitle: "Show", ParentTitle: "S1", Title: "E1"}
	title, season, episode := labels(m)
	if title != "Show" || season != "S1" || episode != "E1" {
		t.Fatalf("unexpected labels for episode: %v %v %v", title, season, episode)
	}

	m2 := plex.Metadata{Type: "movie", Title: "MyMovie"}
	t2, s2, e2 := labels(m2)
	if t2 != "MyMovie" || s2 != "" || e2 != "" {
		t.Fatalf("unexpected labels for movie: %v %v %v", t2, s2, e2)
	}
}

func TestExtrapolatedTransmittedBytes(t *testing.T) {
	s := &sessions{sessions: map[string]session{}, server: &Server{Name: "srv", ID: "id"}}

	// add a playing session that started 2 seconds ago with bitrate 1000
	s.sessions["a"] = session{
		session:     plex.Metadata{Media: []plex.Media{{Bitrate: 1000}}},
		state:       statePlaying,
		playStarted: time.Now().Add(-2 * time.Second),
	}

	out := s.extrapolatedTransmittedBytes()
	if out <= 0 {
		t.Fatalf("expected positive extrapolated bytes, got %v", out)
	}
}

func TestUpdateAccumulatesAndPrunes(t *testing.T) {
	s := &sessions{sessions: map[string]session{}, server: &Server{Name: "srv", ID: "id"}}

	// simulate a playing session that began 1 second ago
	s.sessions["s1"] = session{
		session:     plex.Metadata{Media: []plex.Media{{Bitrate: 500}}},
		state:       statePlaying,
		playStarted: time.Now().Add(-1 * time.Second),
	}

	// Transition to stopped; Update should flatten play time into prevPlayedTime and totalEstimatedTransmittedKBits
	s.Update("s1", stateStopped, nil, nil)

	ss := s.sessions["s1"]
	if ss.prevPlayedTime <= 0 {
		t.Fatalf("expected prevPlayedTime > 0, got %v", ss.prevPlayedTime)
	}
	if s.totalEstimatedTransmittedKBits <= 0 {
		t.Fatalf("expected totalEstimatedTransmittedKBits > 0, got %v", s.totalEstimatedTransmittedKBits)
	}

	// make the stopped session old and prune
	old := s.sessions["s1"]
	old.lastUpdate = time.Now().Add(-2 * sessionTimeout)
	s.sessions["s1"] = old
	s.pruneOldSessions()
	if _, ok := s.sessions["s1"]; ok {
		t.Fatalf("expected session s1 to be pruned")
	}
}
