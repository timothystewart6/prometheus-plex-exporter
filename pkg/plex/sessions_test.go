package plex

import (
	"testing"
	"time"

	jrplex "github.com/jrudio/go-plex-client"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestCollectEmitsTranscodeTypeLabel(t *testing.T) {
	s := &sessions{
		sessions: map[string]session{},
		server: &Server{
			Name: "test-server",
			ID:   "srv-1",
		},
	}

	// Build a minimal session record with required nested fields.
	ss := session{}
	ss.playStarted = time.Now().Add(-5 * time.Second)
	ss.state = statePlaying
	ss.resolvedLibraryName = "lib"
	ss.resolvedLibraryID = "1"
	ss.resolvedLibraryType = "movie"

	// session.session must have Media with Part and Player/User fields used in Collect
	ss.session = jrplex.Metadata{
		Media: []jrplex.Media{{
			Bitrate:         1000,
			VideoResolution: "720p",
			Part: []jrplex.Part{{
				Decision: "transcode",
			}},
		}},
		Player: jrplex.Player{Device: "dev", Product: "plex-player"},
		User:   jrplex.User{Title: "alice"},
	}

	// media metadata used for file resolution
	ss.media = jrplex.Metadata{
		Media: []jrplex.Media{{
			VideoResolution: "1080p",
		}},
	}

	// set persisted transcode type and put into sessions map
	ss.transcodeType = "audio"
	sid := "session-1"
	s.sessions[sid] = ss

	// Collect metrics
	ch := make(chan prometheus.Metric, 10)
	s.Collect(ch)
	close(ch)

	found := false
	for m := range ch {
		var dtoMetric dto.Metric
		if err := m.Write(&dtoMetric); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}
		for _, lp := range dtoMetric.Label {
			if lp.GetName() == "transcode_type" && lp.GetValue() == "audio" {
				found = true
			}
		}
	}

	if !found {
		t.Fatalf("expected collected metrics to include transcode_type=audio label")
	}
}

func TestLabelsFunction(t *testing.T) {
	m := jrplex.Metadata{Type: "episode", GrandparentTitle: "Show", ParentTitle: "S1", Title: "E1"}
	title, season, episode := labels(m)
	if title != "Show" || season != "S1" || episode != "E1" {
		t.Fatalf("unexpected labels for episode: %v %v %v", title, season, episode)
	}

	m2 := jrplex.Metadata{Type: "movie", Title: "MyMovie"}
	t2, s2, e2 := labels(m2)
	if t2 != "MyMovie" || s2 != "" || e2 != "" {
		t.Fatalf("unexpected labels for movie: %v %v %v", t2, s2, e2)
	}
}

func TestExtrapolatedTransmittedBytes(t *testing.T) {
	s := &sessions{sessions: map[string]session{}, server: &Server{Name: "srv", ID: "id"}}

	// add a playing session that started 2 seconds ago with bitrate 1000
	s.sessions["a"] = session{
		session:     jrplex.Metadata{Media: []jrplex.Media{{Bitrate: 1000}}},
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
		session:     jrplex.Metadata{Media: []jrplex.Media{{Bitrate: 500}}},
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
