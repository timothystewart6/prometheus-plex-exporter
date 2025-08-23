package plex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-kit/log"
	jrplex "github.com/jrudio/go-plex-client"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// helper to extract a metric value and label by name
func metricValueAndLabel(m prometheus.Metric, label string) (float64, string, error) {
	var pm dto.Metric
	if err := m.Write(&pm); err != nil {
		return 0, "", err
	}
	var v float64
	if pm.GetCounter() != nil {
		v = pm.GetCounter().GetValue()
	} else if pm.GetGauge() != nil {
		v = pm.GetGauge().GetValue()
	}
	for _, lp := range pm.Label {
		if lp.GetName() == label {
			return v, lp.GetValue(), nil
		}
	}
	return v, "", nil
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

func TestSessionsDescribeAndCollect(t *testing.T) {
	srv := &Server{Name: "srvname", ID: "srvID"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := NewSessions(ctx, srv)
	// Describe
	dch := make(chan *prometheus.Desc, 3)
	s.Describe(dch)
	close(dch)
	descs := 0
	for range dch {
		descs++
	}
	if descs != 3 {
		t.Fatalf("expected 3 descriptors, got %d", descs)
	}

	// Collect should emit at least the estimated transmitted bytes metric
	mch := make(chan prometheus.Metric, 10)
	s.Collect(mch)
	close(mch)
	found := false
	for m := range mch {
		v, lbl, err := metricValueAndLabel(m, "server")
		if err != nil {
			t.Fatalf("failed to read metric: %v", err)
		}
		if lbl == "srvname" || lbl == "srvID" {
			// this is just to reference the labels; continue
		}
		// look for the estimated bytes metric by trying to read value (should be zero)
		if v == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected to find estimated transmitted bytes metric with zero value")
	}
}

func TestListenAlreadyListeningAndNewPlexError(t *testing.T) {
	// Already listening
	s := &Server{listener: &plexListener{}}
	if err := s.Listen(context.Background(), log.NewNopLogger()); !errors.Is(err, ErrAlreadyListening) {
		t.Fatalf("expected ErrAlreadyListening, got %v", err)
	}

	// newPlex failure path
	original := newPlex
	defer func() { newPlex = original }()
	newPlex = func(urlStr, token string) (*jrplex.Plex, error) { return nil, errors.New("boom") }

	u, _ := url.Parse("http://example")
	s2 := &Server{URL: u, Token: "t"}
	if err := s2.Listen(context.Background(), log.NewNopLogger()); err == nil {
		t.Fatalf("expected error from Listen when newPlex fails")
	}
}
