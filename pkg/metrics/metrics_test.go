package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func metricLabelValue(m prometheus.Metric, name string) string {
	var pm dto.Metric
	if err := m.Write(&pm); err != nil {
		return ""
	}
	for _, lp := range pm.Label {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}

func TestPlayDefaultsTranscodeAndSubtitleToNone(t *testing.T) {
	m := Play(1.0, "plex", "srv", "id", "lib", "libid", "libtype", "movie", "Title", "Child", "Grand", "DirectPlay", "1080", "1080", "8000", "Device", "DevType", "user", "sess", "", "")
	if v := metricLabelValue(m, "transcode_type"); v != "none" {
		t.Fatalf("expected transcode_type=none, got %q", v)
	}
	if v := metricLabelValue(m, "subtitle_action"); v != "none" {
		t.Fatalf("expected subtitle_action=none, got %q", v)
	}
}

func TestPlayEmitsProvidedTranscodeAndSubtitle(t *testing.T) {
	m := Play(1.0, "plex", "srv", "id", "lib", "libid", "libtype", "movie", "Title", "Child", "Grand", "transcode", "1080", "1080", "8000", "Device", "DevType", "user", "sess", "audio", "copy")
	if v := metricLabelValue(m, "transcode_type"); v != "audio" {
		t.Fatalf("expected transcode_type=audio, got %q", v)
	}
	if v := metricLabelValue(m, "subtitle_action"); v != "copy" {
		t.Fatalf("expected subtitle_action=copy, got %q", v)
	}
}

func TestPlayDurationDefaultsTranscodeAndSubtitleToNone(t *testing.T) {
	m := PlayDuration(5.0, "plex", "srv", "id", "lib", "libid", "libtype", "movie", "Title", "Child", "Grand", "DirectPlay", "1080", "1080", "8000", "Device", "DevType", "user", "sess", "", "")
	if v := metricLabelValue(m, "transcode_type"); v != "none" {
		t.Fatalf("expected transcode_type=none, got %q", v)
	}
	if v := metricLabelValue(m, "subtitle_action"); v != "none" {
		t.Fatalf("expected subtitle_action=none, got %q", v)
	}
}

func TestLibraryItemsMetric(t *testing.T) {
	m := LibraryItems(15, "plex", "srv", "id123", "movie", "MyLib", "lib1", "movies")
	var metric dto.Metric
	if err := m.Write(&metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric.GetGauge() == nil || metric.GetGauge().GetValue() != 15 {
		t.Fatalf("unexpected gauge value: %v", metric.GetGauge())
	}
}

func TestMediaTotalsMetric(t *testing.T) {
	m := MediaMovies(42, "plex", "srv", "id123")
	var metric dto.Metric
	if err := m.Write(&metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric.GetGauge() == nil || metric.GetGauge().GetValue() != 42 {
		t.Fatalf("unexpected gauge value for movies: %v", metric.GetGauge())
	}

	m2 := MediaEpisodes(100, "plex", "srv", "id123")
	var metric2 dto.Metric
	if err := m2.Write(&metric2); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric2.GetGauge() == nil || metric2.GetGauge().GetValue() != 100 {
		t.Fatalf("unexpected gauge value for episodes: %v", metric2.GetGauge())
	}
}

func TestLibraryDurationAndStorageMetrics(t *testing.T) {
	md := LibraryDuration(12345, "plex", "srv", "id123", "movie", "MyLib", "lib1")
	var m dto.Metric
	if err := md.Write(&m); err != nil {
		t.Fatalf("failed to write library duration metric: %v", err)
	}
	if m.GetGauge() == nil || m.GetGauge().GetValue() != 12345 {
		t.Fatalf("unexpected library duration gauge value: %v", m.GetGauge())
	}

	ms := LibraryStorage(9999, "plex", "srv", "id123", "movie", "MyLib", "lib1")
	var msMetric dto.Metric
	if err := ms.Write(&msMetric); err != nil {
		t.Fatalf("failed to write library storage metric: %v", err)
	}
	if msMetric.GetGauge() == nil || msMetric.GetGauge().GetValue() != 9999 {
		t.Fatalf("unexpected library storage gauge value: %v", msMetric.GetGauge())
	}
}

func TestMediaOtherMetrics(t *testing.T) {
	mm := MediaMusic(7, "plex", "srv", "idX")
	var m dto.Metric
	if err := mm.Write(&m); err != nil {
		t.Fatalf("failed to write media music metric: %v", err)
	}
	if m.GetGauge() == nil || m.GetGauge().GetValue() != 7 {
		t.Fatalf("unexpected media music gauge value: %v", m.GetGauge())
	}

	mp := MediaPhotos(3, "plex", "srv", "idX")
	var mpMetric dto.Metric
	if err := mp.Write(&mpMetric); err != nil {
		t.Fatalf("failed to write media photos metric: %v", err)
	}
	if mpMetric.GetGauge() == nil || mpMetric.GetGauge().GetValue() != 3 {
		t.Fatalf("unexpected media photos gauge value: %v", mpMetric.GetGauge())
	}

	mov := MediaOtherVideos(11, "plex", "srv", "idX")
	var movMetric dto.Metric
	if err := mov.Write(&movMetric); err != nil {
		t.Fatalf("failed to write media other videos metric: %v", err)
	}
	if movMetric.GetGauge() == nil || movMetric.GetGauge().GetValue() != 11 {
		t.Fatalf("unexpected media other videos gauge value: %v", movMetric.GetGauge())
	}
}

func TestRegisterRegistersCollectors(t *testing.T) {
	// Create a dummy collector and register it. If registration fails, MustRegister will panic.
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_register_total"})
	// The Register function wraps prometheus.MustRegister, which will panic on error.
	// Ensure it does not panic for a valid collector.
	Register(c)
}
