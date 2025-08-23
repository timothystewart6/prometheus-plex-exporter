package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

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
