package plex

// NOTE: Test fixtures in this file use synthetic/sanitized machine identifiers
// and other sample values. They are intentionally non-production while
// resembling real data shapes for parsing and logic tests.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"io"

	"github.com/grafana/plexporter/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
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

// fakeTotalsRoundTripper returns canned responses for endpoints used to
// compute server-level movie and episode totals.
type fakeTotalsRoundTripper struct{}

func (f *fakeTotalsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	switch {
	case req.URL.Path == "/media/providers":
		body = `{"MediaContainer": {"friendlyName":"TotalsServer","machineIdentifier":"machine-totals","version":"1.0","MediaProvider":[{"identifier":"com.plexapp.plugins.library","Feature":[{"type":"content","Directory":[{"id":"m1","durationTotal":100,"storageTotal":100,"title":"MoviesA","type":"movie"},{"id":"m2","durationTotal":200,"storageTotal":200,"title":"MoviesB","type":"movie"},{"id":"s1","durationTotal":300,"storageTotal":300,"title":"Shows","type":"show"},{"id":"mu1","durationTotal":0,"storageTotal":0,"title":"Music","type":"music"},{"id":"p1","durationTotal":0,"storageTotal":0,"title":"Photos","type":"photo"},{"id":"hv1","durationTotal":0,"storageTotal":0,"title":"HomeVideos","type":"homevideo"}]}]}]}}`
	case req.URL.Path == "/library/sections/m1/all":
		body = `{"MediaContainer":{"size":7}}`
	case req.URL.Path == "/library/sections/m2/all":
		body = `{"MediaContainer":{"size":3}}`
	case req.URL.Path == "/library/sections/s1/all" && req.URL.RawQuery == "type=4":
		// Filtered episodes request
		body = `{"MediaContainer":{"size":42}}`
	case req.URL.Path == "/library/sections/s1/all":
		// Unfiltered call used by Refresh(); return 0 to force the
		// filtered type=4 request to be used for episode counting.
		body = `{"MediaContainer":{"size":0}}`
	case req.URL.Path == "/library/sections/mu1/all" && req.URL.RawQuery == "type=10":
		// music tracks (type=10)
		body = `{"MediaContainer":{"size":123}}`
	case req.URL.Path == "/library/sections/mu1/all":
		body = `{"MediaContainer":{"size":0}}`
	case req.URL.Path == "/library/sections/p1/all":
		body = `{"MediaContainer":{"size":55}}`
	case req.URL.Path == "/library/sections/hv1/all":
		body = `{"MediaContainer":{"size":9}}`
	case req.URL.RawQuery == "":
		// fallback
		body = `{"MediaContainer":{}}`
	default:
		// handle the filtered episodes call
		if req.URL.Path == "/library/sections/s1/all" && req.URL.RawQuery == "type=4" {
			body = `{"MediaContainer":{"size":42}}`
		} else if req.URL.Path == "/" {
			body = `{"MediaContainer":{"version":"v","platform":"p","platformVersion":"pv"}}`
		} else if req.URL.Path == "/statistics/resources" {
			body = `{"MediaContainer":{"StatisticsResources":[]}}`
		} else if req.URL.Path == "/statistics/bandwidth" {
			body = `{"MediaContainer":{"StatisticsBandwith":[]}}`
		} else {
			body = `{"MediaContainer":{}}`
		}
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

func TestServerTotalsComputed(t *testing.T) {
	client, err := NewClient("http://example.com", "token")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	// Replace the http client transport with our fake RT
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

	// Movie totals: m1 (7) + m2 (3) = 10
	if srv.MovieCount != 10 {
		t.Fatalf("expected MovieCount 10, got %d", srv.MovieCount)
	}

	// Episode totals: s1 type=4 returns 42
	if srv.EpisodeCount != 42 {
		t.Fatalf("expected EpisodeCount 42, got %d", srv.EpisodeCount)
	}
}

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

func TestNewServerRefreshPopulatesFields(t *testing.T) {
	// httptest server that responds to the endpoints used by Refresh
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media/providers":
			// includeStorage=1 may be in RawQuery; respond with one movie library
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"MediaContainer": {"friendlyName": "MyServer", "machineIdentifier":"ID123", "version":"1.0", "MediaProvider":[{"identifier":"com.plexapp.plugins.library","Feature":[{"type":"content","Directory":[{"id":"1","durationTotal":1000,"storageTotal":2000,"title":"Lib1","type":"movie"}]}]}]}}`))
		case "/library/sections/1/all":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"MediaContainer": {"size": 5}}`))
		case "/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"MediaContainer": {"version": "1.0", "platform": "darwin", "platformVersion": "13.0"}}`))
		case "/statistics/resources":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"MediaContainer": {"StatisticsResources": [{"at": 1, "hostCpuUtilization": 1.2, "hostMemoryUtilization": 2.3}]}}`))
		case "/statistics/bandwidth":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"MediaContainer": {"StatisticsBandwith": [{"at": 2, "bytes": 100}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	serverURL := ts.URL
	srv, err := NewServer(serverURL, "token")
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Give a small amount of time for the initial Refresh goroutine to finish
	time.Sleep(50 * time.Millisecond)

	if srv.Name != "MyServer" {
		t.Fatalf("expected server name MyServer, got %q", srv.Name)
	}
	if srv.ID != "ID123" {
		t.Fatalf("expected server ID ID123, got %q", srv.ID)
	}
	if srv.MovieCount != 5 {
		t.Fatalf("expected MovieCount 5, got %d", srv.MovieCount)
	}
}
