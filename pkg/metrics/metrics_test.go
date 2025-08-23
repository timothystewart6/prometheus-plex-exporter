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
	m := LibraryItems(15, "plex", "srv", "id123", "movie", "MyLib", "lib1")
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
