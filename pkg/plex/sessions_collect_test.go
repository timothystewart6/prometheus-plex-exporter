package plex

// NOTE: Test fixtures in this file use synthetic/sanitized session keys and
// other identifiers so tests exercise collection logic without real data.

import (
	"context"
	"testing"
	"time"

	ttPlex "github.com/timothystewart6/go-plex-client"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func readMetric(m prometheus.Metric) (float64, map[string]string, error) {
	var pm dto.Metric
	if err := m.Write(&pm); err != nil {
		return 0, nil, err
	}
	var v float64
	if pm.GetCounter() != nil {
		v = pm.GetCounter().GetValue()
	} else if pm.GetGauge() != nil {
		v = pm.GetGauge().GetValue()
	}
	labels := map[string]string{}
	for _, lp := range pm.Label {
		labels[lp.GetName()] = lp.GetValue()
	}
	return v, labels, nil
}

func TestSessionsCollectEmitsPlayAndPlayDuration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &Server{Name: "srvname", ID: "srvID"}
	s := NewSessions(ctx, srv)

	// Build session and media metadata
	mdSession := ttPlex.Metadata{
		SessionKey: "sess-1",
		Media: []ttPlex.Media{{
			Part:            []ttPlex.Part{{Decision: "DirectPlay"}},
			VideoResolution: "1080",
			Bitrate:         8000,
		}},
		Player: ttPlex.Player{Device: "DeviceX", Product: "DeviceType"},
		User:   ttPlex.User{Title: "alice"},
	}
	mdMedia := ttPlex.Metadata{
		Type:             "movie",
		Title:            "MyMovie",
		LibrarySectionID: ttPlex.FlexibleInt64(1),
		Media:            []ttPlex.Media{{VideoResolution: "1080"}},
	}

	// Insert session with a non-zero playStarted so Collect will emit metrics
	s.mtx.Lock()
	s.sessions["sess-1"] = session{
		session:             mdSession,
		media:               mdMedia,
		state:               statePlaying,
		playStarted:         time.Now().Add(-5 * time.Second),
		prevPlayedTime:      2 * time.Second,
		resolvedLibraryName: "MyLib",
		resolvedLibraryID:   "1",
		resolvedLibraryType: "movie",
		transcodeType:       "none",
		subtitleAction:      "none",
	}
	s.mtx.Unlock()

	ch := make(chan prometheus.Metric, 10)
	s.Collect(ch)
	close(ch)

	foundPlay := false
	foundPlayDuration := false
	for m := range ch {
		v, labels, err := readMetric(m)
		if err != nil {
			t.Fatalf("failed to read metric: %v", err)
		}
		if labels["title"] == "MyMovie" {
			// There should be a Play (counter) and a PlayDuration (counter) metric
			if labels["user"] != "alice" {
				t.Fatalf("expected user label alice, got %q", labels["user"])
			}
			// Value should be >= 0
			if v < 0 {
				t.Fatalf("unexpected negative metric value: %v", v)
			}
			// Determine whether this metric is play count or play duration by checking label presence
			if _, ok := labels["session"]; ok {
				// Both metrics include 'session' label; distinguish by value: Play should be 1.0, PlayDuration > 0
				if v == 1.0 {
					foundPlay = true
				} else if v > 0 {
					foundPlayDuration = true
				}
			}
		}
	}
	if !foundPlay {
		t.Fatalf("expected Play metric for session not found")
	}
	if !foundPlayDuration {
		t.Fatalf("expected PlayDuration metric for session not found")
	}
}
