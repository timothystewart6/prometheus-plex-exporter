package plex

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"testing"
	"time"
)

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
		Body:       ioutil.NopCloser(bytes.NewBufferString(body)),
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
