package metrics

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
)

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
