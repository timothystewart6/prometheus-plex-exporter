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
	url := req.URL.Path
	if req.URL.RawQuery != "" {
		url += "?" + req.URL.RawQuery
	}

	switch url {
	case "/media/providers", "/media/providers?includeStorage=1":
		body = `{"MediaContainer": {"friendlyName":"TestServer","machineIdentifier":"machine123","version":"1.2.3","MediaProvider":[{"identifier":"com.plexapp.plugins.library","Feature":[{"type":"content","Directory":[{"id":"1","durationTotal":1000,"storageTotal":2000,"title":"Movies","type":"movie"},{"id":"2","durationTotal":2000,"storageTotal":3000,"title":"Shows","type":"show"}]}]}]}}`
	case "/library/sections/1/all":
		body = `{"MediaContainer":{"size":10}}`
	case "/library/sections/2/all":
		body = `{"MediaContainer":{"size":20}}`
	case "/":
		body = `{"MediaContainer":{"version":"v","platform":"p","platformVersion":"pv"}}`
	case "/statistics/resources":
		body = `{"MediaContainer":{"StatisticsResources":[{"at":1,"hostCpuUtilization":5,"hostMemoryUtilization":6}]}}`
	case "/statistics/bandwidth?timespan=6":
		body = `{"MediaContainer":{"StatisticsBandwidth":[{"at":100,"bytes":1000},{"at":200,"bytes":2000}]}}`
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

// TestRefreshBandwidthSuccess tests the successful refresh of bandwidth statistics.
func TestRefreshBandwidthSuccess(t *testing.T) {
	client, err := NewClient("http://example.com", "token")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	client.httpClient = http.Client{Transport: &fakeRoundTripper{}, Timeout: time.Second * 2}

	srv := &Server{
		URL:             client.URL,
		Token:           client.Token,
		Client:          client,
		Name:            "TestServer",
		ID:              "server123",
		lastBandwidthAt: 50, // Set to before our test data timestamps (100, 200)
	}

	// Test refreshBandwidth
	if err := srv.Refresh(); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}

	// Verify lastBandwidthAt was updated to the highest timestamp
	// The test data in fakeRoundTripper has timestamps 100 and 200
	if srv.lastBandwidthAt != 200 {
		t.Fatalf("expected lastBandwidthAt=200, got %d", srv.lastBandwidthAt)
	}
}

// TestRefreshBandwidthSimple tests bandwidth logic with a more direct approach.
func TestRefreshBandwidthSimple(t *testing.T) {
	// Create a very simple test that verifies bandwidth timestamps get updated
	simpleRT := &customRoundTripper{
		responses: map[string]response{
			"/media/providers":                 {statusCode: 200, body: `{"MediaContainer": {"friendlyName":"TestServer","machineIdentifier":"machine123","version":"1.2.3","MediaProvider":[{"identifier":"com.plexapp.plugins.library","Feature":[]}]}}`},
			"/statistics/resources":            {statusCode: 200, body: `{"MediaContainer":{"StatisticsResources":[]}}`},
			"/statistics/bandwidth?timespan=6": {statusCode: 200, body: `{"MediaContainer":{"StatisticsBandwidth":[{"at":300,"bytes":3000}]}}`},
		},
	}

	client, err := NewClient("http://example.com", "token")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	client.httpClient = http.Client{Transport: simpleRT, Timeout: time.Second * 2}

	srv := &Server{
		URL:             client.URL,
		Token:           client.Token,
		Client:          client,
		Name:            "TestServer",
		ID:              "server123",
		lastBandwidthAt: 250, // Set to before our test timestamp (300)
	}

	if err := srv.Refresh(); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}

	// Verify lastBandwidthAt was updated to 300
	if srv.lastBandwidthAt != 300 {
		t.Fatalf("expected lastBandwidthAt=300, got %d", srv.lastBandwidthAt)
	}
}

// TestRefreshBandwidthNotFound tests bandwidth refresh when endpoint returns 404.
func TestRefreshBandwidthNotFound(t *testing.T) {
	// Create a custom round tripper that returns 404 for bandwidth endpoint
	notFoundRT := &customRoundTripper{
		responses: map[string]response{
			"/statistics/bandwidth?timespan=6": {statusCode: 404, body: ""},
		},
	}

	client, err := NewClient("http://example.com", "token")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	client.httpClient = http.Client{Transport: notFoundRT, Timeout: time.Second * 2}

	srv := &Server{
		URL:             client.URL,
		Token:           client.Token,
		Client:          client,
		Name:            "TestServer",
		ID:              "server123",
		lastBandwidthAt: 50,
	}

	// This should not return an error for 404 (paid feature not available)
	if err := srv.refreshBandwidth(); err != nil {
		t.Fatalf("expected no error for 404 response, got: %v", err)
	}

	// lastBandwidthAt should remain unchanged
	if srv.lastBandwidthAt != 50 {
		t.Fatalf("expected lastBandwidthAt=50, got %d", srv.lastBandwidthAt)
	}
}

// TestRefreshBandwidthEmptyData tests bandwidth refresh with empty data.
func TestRefreshBandwidthEmptyData(t *testing.T) {
	emptyRT := &customRoundTripper{
		responses: map[string]response{
			"/statistics/bandwidth?timespan=6": {statusCode: 200, body: `{"MediaContainer":{"StatisticsBandwidth":[]}}`},
		},
	}

	client, err := NewClient("http://example.com", "token")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	client.httpClient = http.Client{Transport: emptyRT, Timeout: time.Second * 2}

	srv := &Server{
		URL:             client.URL,
		Token:           client.Token,
		Client:          client,
		Name:            "TestServer",
		ID:              "server123",
		lastBandwidthAt: 50,
	}

	if err := srv.Refresh(); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}

	// lastBandwidthAt should be reset to 0 when no bandwidth data is processed
	if srv.lastBandwidthAt != 0 {
		t.Fatalf("expected lastBandwidthAt=0 (reset when no new data), got %d", srv.lastBandwidthAt)
	}
}

// customRoundTripper allows custom responses for specific paths
type customRoundTripper struct {
	responses map[string]response
}

type response struct {
	statusCode int
	body       string
}

func (c *customRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	url := req.URL.Path
	if req.URL.RawQuery != "" {
		url += "?" + req.URL.RawQuery
	}

	if resp, ok := c.responses[url]; ok {
		return &http.Response{
			StatusCode: resp.statusCode,
			Status:     http.StatusText(resp.statusCode),
			Body:       io.NopCloser(bytes.NewBufferString(resp.body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}

	// Default response
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewBufferString(`{"MediaContainer":{}}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
