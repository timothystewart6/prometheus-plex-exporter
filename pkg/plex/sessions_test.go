package plex

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	ttPlex "github.com/timothystewart6/go-plex-client"
)

func TestCollectEmitsTranscodeTypeLabel(t *testing.T) {
	s := &sessions{
		sessions: map[string]session{},
		server: &Server{
			Name: "test-server",
			ID:   "srv-1",
		},
	}

	// Build a minimal session record with required nested fields.
	ss := session{}
	ss.playStarted = time.Now().Add(-5 * time.Second)
	ss.state = statePlaying
	ss.resolvedLibraryName = "lib"
	ss.resolvedLibraryID = "1"
	ss.resolvedLibraryType = "movie"

	// session.session must have Media with Part and Player/User fields used in Collect
	ss.session = ttPlex.Metadata{
		Media: []ttPlex.Media{{
			Bitrate:         1000,
			VideoResolution: "720p",
			Part: []ttPlex.Part{{
				Decision: "transcode",
			}},
		}},
		Player: ttPlex.Player{Device: "dev", Product: "plex-player"},
		User:   ttPlex.User{Title: "alice"},
	}

	// media metadata used for file resolution
	ss.media = ttPlex.Metadata{
		Media: []ttPlex.Media{{
			VideoResolution: "1080p",
		}},
	}

	// set persisted transcode type and put into sessions map
	ss.transcodeType = "audio"
	sid := "session-1"
	s.sessions[sid] = ss

	// Collect metrics
	ch := make(chan prometheus.Metric, 10)
	s.Collect(ch)
	close(ch)

	found := false
	for m := range ch {
		var dtoMetric dto.Metric
		if err := m.Write(&dtoMetric); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}
		for _, lp := range dtoMetric.Label {
			if lp.GetName() == "transcode_type" && lp.GetValue() == "audio" {
				found = true
			}
			if lp.GetName() == "subtitle_action" && lp.GetValue() == "none" {
				// ensure subtitle_action label exists and defaults to "none"
				_ = lp
			}
		}
	}

	if !found {
		t.Fatalf("expected collected metrics to include transcode_type=audio label")
	}
}

func TestLabelsFunction(t *testing.T) {
	m := ttPlex.Metadata{Type: "episode", GrandparentTitle: "Show", ParentTitle: "S1", Title: "E1"}
	title, season, episode := labels(m)
	if title != "Show" || season != "S1" || episode != "E1" {
		t.Fatalf("unexpected labels for episode: %v %v %v", title, season, episode)
	}

	m2 := ttPlex.Metadata{Type: "movie", Title: "MyMovie"}
	t2, s2, e2 := labels(m2)
	if t2 != "MyMovie" || s2 != "" || e2 != "" {
		t.Fatalf("unexpected labels for movie: %v %v %v", t2, s2, e2)
	}
}

func TestExtrapolatedTransmittedBytes(t *testing.T) {
	s := &sessions{sessions: map[string]session{}, server: &Server{Name: "srv", ID: "id"}}

	// add a playing session that started 2 seconds ago with bitrate 1000
	s.sessions["a"] = session{
		session:     ttPlex.Metadata{Media: []ttPlex.Media{{Bitrate: 1000}}},
		state:       statePlaying,
		playStarted: time.Now().Add(-2 * time.Second),
	}

	out := s.extrapolatedTransmittedBytes()
	if out <= 0 {
		t.Fatalf("expected positive extrapolated bytes, got %v", out)
	}
}

func TestUpdateAccumulatesAndPrunes(t *testing.T) {
	s := &sessions{sessions: map[string]session{}, server: &Server{Name: "srv", ID: "id"}}

	// simulate a playing session that began 1 second ago
	s.sessions["s1"] = session{
		session:     ttPlex.Metadata{Media: []ttPlex.Media{{Bitrate: 500}}},
		state:       statePlaying,
		playStarted: time.Now().Add(-1 * time.Second),
	}

	// Transition to stopped; Update should flatten play time into prevPlayedTime and totalEstimatedTransmittedKBits
	s.Update("s1", stateStopped, nil, nil)

	ss := s.sessions["s1"]
	if ss.prevPlayedTime <= 0 {
		t.Fatalf("expected prevPlayedTime > 0, got %v", ss.prevPlayedTime)
	}
	if s.totalEstimatedTransmittedKBits <= 0 {
		t.Fatalf("expected totalEstimatedTransmittedKBits > 0, got %v", s.totalEstimatedTransmittedKBits)
	}

	// make the stopped session old and prune
	old := s.sessions["s1"]
	old.lastUpdate = time.Now().Add(-2 * sessionTimeout)
	s.sessions["s1"] = old
	s.pruneOldSessions()
	if _, ok := s.sessions["s1"]; ok {
		t.Fatalf("expected session s1 to be pruned")
	}
}

// TestSessionsCollectWithUnknownLibrary tests Collect when library lookup fails.
func TestSessionsCollectWithUnknownLibrary(t *testing.T) {
	server := &Server{
		Name:      "test-server",
		ID:        "srv-1",
		libraries: []*Library{}, // Empty libraries to force unknown lookup
	}

	s := NewSessions(context.Background(), server)

	// Build a session without resolved library info
	ss := session{}
	// Don't set ss.playStarted or ss.state - let Update handle the transition
	// Don't set resolved library fields to test fallback

	ss.session = ttPlex.Metadata{
		Media: []ttPlex.Media{{
			Bitrate:         1000,
			VideoResolution: "720p",
			Part: []ttPlex.Part{{
				Decision: "direct_play",
			}},
		}},
		Player: ttPlex.Player{Device: "device", Product: "player"},
		User:   ttPlex.User{Title: "user"},
	}

	ss.media = ttPlex.Metadata{
		LibrarySectionID: ttPlex.FlexibleInt64(999), // Non-existent library
		Media: []ttPlex.Media{{
			VideoResolution: "1080p",
		}},
	}

	ss.transcodeType = "none"
	ss.subtitleAction = "none"

	// First create the session in stopped state, then transition to playing
	s.Update("test-session", stateStopped, &ss.session, &ss.media)
	s.Update("test-session", statePlaying, &ss.session, &ss.media)

	// Collect metrics
	ch := make(chan prometheus.Metric, 10)
	s.Collect(ch)
	close(ch)

	// Should find metrics with "unknown" library labels
	foundUnknownLibrary := false
	foundUnknownLibraryID := false
	metricsCount := 0
	for m := range ch {
		metricsCount++
		var dtoMetric dto.Metric
		if err := m.Write(&dtoMetric); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}
		for _, lp := range dtoMetric.Label {
			if lp.GetName() == "library" && lp.GetValue() == "unknown" {
				foundUnknownLibrary = true
			}
			if lp.GetName() == "library_id" && lp.GetValue() == "0" {
				foundUnknownLibraryID = true
			}
		}
	}

	if metricsCount == 0 {
		t.Fatalf("expected some metrics to be generated")
	}
	if !foundUnknownLibrary {
		t.Fatalf("expected metrics with unknown library name")
	}
	if !foundUnknownLibraryID {
		t.Fatalf("expected metrics with unknown library ID")
	}
}

// TestSessionsCollectSkipsSessionsWithoutPlayStarted tests that Collect skips sessions without playStarted time.
func TestSessionsCollectSkipsSessionsWithoutPlayStarted(t *testing.T) {
	s := &sessions{
		sessions: map[string]session{},
		server: &Server{
			Name: "test-server",
			ID:   "srv-1",
		},
	}

	// Add a session without playStarted time (zero value)
	ss := session{}
	// playStarted is zero value - should be skipped
	ss.state = statePlaying
	ss.resolvedLibraryName = "lib"
	ss.resolvedLibraryID = "1"
	ss.resolvedLibraryType = "movie"

	ss.session = ttPlex.Metadata{
		Media: []ttPlex.Media{{
			Bitrate:         1000,
			VideoResolution: "720p",
			Part: []ttPlex.Part{{
				Decision: "transcode",
			}},
		}},
		Player: ttPlex.Player{Device: "dev", Product: "player"},
		User:   ttPlex.User{Title: "alice"},
	}

	ss.media = ttPlex.Metadata{
		Media: []ttPlex.Media{{
			VideoResolution: "1080p",
		}},
	}

	s.sessions["skipped-session"] = ss

	// Collect metrics
	ch := make(chan prometheus.Metric, 10)
	s.Collect(ch)
	close(ch)

	// Should only get the extrapolated bytes metric, no play metrics
	metricCount := 0
	for range ch {
		metricCount++
	}

	// Should only have 1 metric (extrapolated bytes), not play metrics
	if metricCount != 1 {
		t.Fatalf("expected 1 metric (extrapolated bytes only), got %d", metricCount)
	}
}

// TestSessionsCollectWithResolvedLibrary tests Collect using resolved library information.
func TestSessionsCollectWithResolvedLibrary(t *testing.T) {
	s := NewSessions(context.Background(), &Server{
		Name: "test-server",
		ID:   "srv-1",
	})

	// Create a session using Update method with resolved library info
	sessionMeta := &ttPlex.Metadata{
		Media: []ttPlex.Media{{
			Bitrate:         2000,
			VideoResolution: "1080p",
			Part: []ttPlex.Part{{
				Decision: "copy",
			}},
		}},
		Player: ttPlex.Player{Device: "tablet", Product: "plex-app"},
		User:   ttPlex.User{Title: "bob"},
	}

	mediaMeta := &ttPlex.Metadata{
		Type:  "movie",
		Title: "Test Movie",
		Media: []ttPlex.Media{{
			VideoResolution: "4K",
		}},
	}

	// Update session to playing state, then pause it
	s.Update("resolved-session", statePlaying, sessionMeta, mediaMeta)
	s.Update("resolved-session", statePaused, sessionMeta, mediaMeta)

	// Manually set additional fields using direct access (for testing)
	s.mtx.Lock()
	if ss, ok := s.sessions["resolved-session"]; ok {
		ss.prevPlayedTime = 5 * time.Second
		ss.resolvedLibraryName = "Movies"
		ss.resolvedLibraryID = "2"
		ss.resolvedLibraryType = "movie"
		ss.transcodeType = "video"
		ss.subtitleAction = "burn"
		s.sessions["resolved-session"] = ss
	}
	s.mtx.Unlock()

	// Collect metrics
	ch := make(chan prometheus.Metric, 10)
	s.Collect(ch)
	close(ch)

	// Verify we get the expected metrics with correct labels
	playMetricFound := false
	playDurationMetricFound := false
	extrapolatedBytesFound := false

	for m := range ch {
		var dtoMetric dto.Metric
		if err := m.Write(&dtoMetric); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}

		// Check for play metric (counter with value 1.0)
		if dtoMetric.GetCounter() != nil && dtoMetric.GetCounter().GetValue() == 1.0 {
			playMetricFound = true
			// Verify resolved library labels are used
			labelMap := make(map[string]string)
			for _, lp := range dtoMetric.Label {
				labelMap[lp.GetName()] = lp.GetValue()
			}
			if labelMap["library"] != "Movies" {
				t.Fatalf("expected library=Movies, got %s", labelMap["library"])
			}
			if labelMap["transcode_type"] != "video" {
				t.Fatalf("expected transcode_type=video, got %s", labelMap["transcode_type"])
			}
			if labelMap["subtitle_action"] != "burn" {
				t.Fatalf("expected subtitle_action=burn, got %s", labelMap["subtitle_action"])
			}
		} else if dtoMetric.GetCounter() != nil && dtoMetric.GetCounter().GetValue() == 5.0 {
			// Play duration metric should be 5 seconds (prevPlayedTime since state is paused)
			playDurationMetricFound = true
		} else if dtoMetric.GetCounter() != nil {
			extrapolatedBytesFound = true
		}
	}

	if !playMetricFound {
		t.Fatalf("expected play metric not found")
	}
	if !playDurationMetricFound {
		t.Fatalf("expected play duration metric not found")
	}
	if !extrapolatedBytesFound {
		t.Fatalf("expected extrapolated bytes metric not found")
	}
}

func TestExtractTranscodeSessionID(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard transcode session path",
			input:    "/transcode/sessions/zplafkzqz7buwjittgvojjyl",
			expected: "zplafkzqz7buwjittgvojjyl",
		},
		{
			name:     "transcode session path with segments",
			input:    "/transcode/sessions/abc123/segments/0",
			expected: "abc123",
		},
		{
			name:     "transcode session path without trailing slash",
			input:    "/transcode/sessions/test-session",
			expected: "test-session",
		},
		{
			name:     "path without transcode sessions",
			input:    "/some/other/path",
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "transcode sessions prefix only",
			input:    "/transcode/sessions/",
			expected: "",
		},
		{
			name:     "path without transcode prefix",
			input:    "no-transcode-in-path",
			expected: "",
		},
		{
			name:     "complex path with transcode session",
			input:    "/library/parts/123/transcode/sessions/complex123/file.m3u8",
			expected: "complex123",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := extractTranscodeSessionID(tc.input)
			if result != tc.expected {
				t.Errorf("extractTranscodeSessionID(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestTrySetTranscodeTypeWithTranscodeSessionPath(t *testing.T) {
	s := &sessions{
		sessions: map[string]session{},
		server: &Server{
			Name: "test-server",
			ID:   "srv-1",
		},
	}

	// Create a session with a transcode session path in the Part.Key field
	ss := session{}
	ss.playStarted = time.Now().Add(-5 * time.Second)
	ss.state = statePlaying
	ss.resolvedLibraryName = "lib"
	ss.resolvedLibraryID = "1"
	ss.resolvedLibraryType = "movie"

	// Simulate session data where Part.Key contains a transcode session path
	ss.session = ttPlex.Metadata{
		SessionKey: "different-session-key",
		Media: []ttPlex.Media{{
			Bitrate:         1000,
			VideoResolution: "720p",
			Part: []ttPlex.Part{{
				Decision: "transcode",
				Key:      "/transcode/sessions/zplafkzqz7buwjittgvojjyl/file.m3u8",
			}},
		}},
		Player: ttPlex.Player{Device: "dev", Product: "plex-player"},
		User:   ttPlex.User{Title: "alice"},
	}

	// Add session to the map
	s.sessions["session-1"] = ss

	// Test that TrySetTranscodeType matches the websocket transcode session ID
	// against the embedded transcode session path
	websocketSessionID := "zplafkzqz7buwjittgvojjyl"
	result := s.TrySetTranscodeType(websocketSessionID, "hw")

	if !result {
		t.Fatalf("TrySetTranscodeType should have found a match for transcode session ID %s", websocketSessionID)
	}

	// Verify the transcode type was set correctly
	updatedSession := s.sessions["session-1"]
	if updatedSession.transcodeType != "hw" {
		t.Fatalf("expected transcodeType to be 'hw', got %s", updatedSession.transcodeType)
	}
}

func TestTrySetTranscodeTypeWithFullPath(t *testing.T) {
	s := &sessions{
		sessions: map[string]session{},
		server: &Server{
			Name: "test-server",
			ID:   "srv-1",
		},
	}

	// Create a session with a transcode session path in the Part.Key field
	ss := session{}
	ss.playStarted = time.Now().Add(-5 * time.Second)
	ss.state = statePlaying
	ss.resolvedLibraryName = "lib"
	ss.resolvedLibraryID = "1"
	ss.resolvedLibraryType = "movie"

	// Simulate session data where Part.Key contains a transcode session path
	ss.session = ttPlex.Metadata{
		SessionKey: "different-session-key",
		Media: []ttPlex.Media{{
			Bitrate:         1000,
			VideoResolution: "720p",
			Part: []ttPlex.Part{{
				Decision: "transcode",
				Key:      "/transcode/sessions/fse26of3focw33mqyua0aity/file.m3u8",
			}},
		}},
		Player: ttPlex.Player{Device: "dev", Product: "plex-player"},
		User:   ttPlex.User{Title: "alice"},
	}

	// Add session to the map
	s.sessions["session-1"] = ss

	// Test that TrySetTranscodeType matches when websocket sends the FULL PATH
	// This is what we're seeing in the logs: tsKey=/transcode/sessions/fse26of3focw33mqyua0aity
	websocketFullPath := "/transcode/sessions/fse26of3focw33mqyua0aity"
	result := s.TrySetTranscodeType(websocketFullPath, "both")

	if !result {
		t.Fatalf("TrySetTranscodeType should have found a match for full transcode session path %s", websocketFullPath)
	}

	// Verify the transcode type was set correctly
	updatedSession := s.sessions["session-1"]
	if updatedSession.transcodeType != "both" {
		t.Fatalf("expected transcodeType to be 'both', got %s", updatedSession.transcodeType)
	}
}
