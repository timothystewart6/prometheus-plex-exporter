package plex

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/plexporter/pkg/metrics"
	dto "github.com/prometheus/client_model/go"
)

// fakeRoundTripper returns canned responses for known Plex endpoints
type fakeRoundTripper struct{}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	switch req.URL.Path {
	case "/media/providers":
		body = `{"MediaContainer": {"friendlyName":"TestServer","machineIdentifier":"machine123","version":"1.2.3","MediaProvider":[{"identifier":"com.plexapp.plugins.library","Feature":[{"type":"content","Directory":[{"id":"1","durationTotal":1000,"storageTotal":2000,"title":"Movies","type":"movie"},{"id":"2","durationTotal":2000,"storageTotal":3000,"title":"Shows","type":"show"}]}]}]}}`
	case "/library/sections/1/all":
		body = `{"MediaContainer":{"size":10}}`
	case "/library/sections/2/all":
		body = `{"MediaContainer":{"size":20}}`
	case "/":
		body = `{"MediaContainer":{"version":"v","platform":"p","platformVersion":"pv"}}`
	case "/statistics/resources":
		body = `{"MediaContainer":{"StatisticsResources":[{"at":1,"hostCpuUtilization":5,"hostMemoryUtilization":6}]}}`
	case "/statistics/bandwidth":
		body = `{"MediaContainer":{"StatisticsBandwith":[]}}`
	default:
		body = `{"MediaContainer":{}}`
	}

	resp := &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
		Request:    req,
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

func TestServerRefreshPopulatesLibraryItems(t *testing.T) {
	client, err := NewClient("http://example.com", "token")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	// Replace the http client transport with our fake RT
	client.httpClient = http.Client{Transport: &fakeRoundTripper{}, Timeout: time.Second * 2}

	srv := &Server{
		URL:             client.URL,
		Token:           client.Token,
		Client:          client,
		lastBandwidthAt: int(time.Now().Unix()),
	}

	if err := srv.Refresh(); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}

	if len(srv.libraries) != 2 {
		t.Fatalf("expected 2 libraries, got %d", len(srv.libraries))
	}

	// Check item counts for each library
	expected := map[string]int64{"1": 10, "2": 20}
	for _, lib := range srv.libraries {
		want, ok := expected[lib.ID]
		if !ok {
			t.Fatalf("unexpected library id %s", lib.ID)
		}
		if lib.ItemsCount != want {
			t.Fatalf("library %s items: want %d got %d", lib.ID, want, lib.ItemsCount)
		}

		// Validate the metric produced by metrics.LibraryItems matches value & labels
		m := metrics.LibraryItems(lib.ItemsCount, "plex", srv.Name, srv.ID, lib.Type, lib.Name, lib.ID)
		var dtoMetric dto.Metric
		if err := m.Write(&dtoMetric); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}

		if dtoMetric.GetGauge() == nil {
			t.Fatalf("expected gauge metric")
		}
		if dtoMetric.GetGauge().GetValue() != float64(want) {
			t.Fatalf("metric value mismatch: want %v got %v", want, dtoMetric.GetGauge().GetValue())
		}

		// Ensure some expected labels are present
		labels := map[string]bool{}
		for _, lp := range dtoMetric.Label {
			labels[lp.GetValue()] = true
		}
		if !labels[srv.Name] || !labels[srv.ID] || !labels[lib.Name] || !labels[lib.ID] {
			t.Fatalf("expected labels missing in metric: %v", dtoMetric.Label)
		}
	}
}
