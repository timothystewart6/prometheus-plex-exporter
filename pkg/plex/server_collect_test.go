package plex

import (
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Test that Collect emits server-level media totals for all supported types.
func TestServerCollectEmitsMediaTotals(t *testing.T) {
	client, err := NewClient("http://example.com", "token")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	// Use the fake totals round tripper which returns canned sizes for our
	// movie/show/music/photo/homevideo libraries and filtered track/episode calls.
	client.httpClient = http.Client{Transport: &fakeTotalsRoundTripper{}, Timeout: time.Second * 2}

	srv := &Server{
		URL:             client.URL,
		Token:           client.Token,
		Client:          client,
		lastBandwidthAt: int(time.Now().Unix()),
	}

	if err := srv.Refresh(); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}

	t.Logf("computed totals: movies=%d episodes=%d music=%d photos=%d other=%d", srv.MovieCount, srv.EpisodeCount, srv.MusicCount, srv.PhotoCount, srv.OtherVideoCount)

	reg := prometheus.NewRegistry()
	if err := reg.Register(srv); err != nil {
		t.Fatalf("failed to register server collector: %v", err)
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	// Helper to find metric family by name
	find := func(name string) *dto.MetricFamily {
		for _, mf := range mfs {
			if mf.GetName() == name {
				return mf
			}
		}
		return nil
	}

	checks := []struct {
		name string
		want float64
	}{
		{"plex_media_movies", 10},
		{"plex_media_episodes", 42},
		{"plex_media_music", 123},
		{"plex_media_photos", 55},
		{"plex_media_other_videos", 9},
	}

	for _, c := range checks {
		mf := find(c.name)
		if mf == nil {
			t.Fatalf("metric family %s not found", c.name)
		}
		if len(mf.Metric) == 0 {
			t.Fatalf("no metrics in family %s", c.name)
		}
		// Metric family is expected to have a single gauge metric with our server labels
		m := mf.Metric[0]
		var got float64
		if m.GetGauge() != nil {
			got = m.GetGauge().GetValue()
		} else if m.GetCounter() != nil {
			got = m.GetCounter().GetValue()
		} else {
			t.Fatalf("metric %s has no gauge or counter", c.name)
		}
		if got != c.want {
			t.Fatalf("metric %s: want %v got %v", c.name, c.want, got)
		}
	}
}
