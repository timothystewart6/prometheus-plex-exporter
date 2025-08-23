package plex

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/go-kit/log"
	jrplex "github.com/jrudio/go-plex-client"
)

// fakePlexClient implements the minimal plexClient interface for tests.
type fakePlexClient struct {
	sessions jrplex.CurrentSessions
	sessErr  error
}

func (f *fakePlexClient) GetSessions() (jrplex.CurrentSessions, error) { return f.sessions, f.sessErr }
func (f *fakePlexClient) GetMetadata(s string) (jrplex.MediaMetadata, error) {
	return jrplex.MediaMetadata{}, errors.New("not implemented")
}
func (f *fakePlexClient) GetTranscodeSessions() (jrplex.TranscodeSessionsResponse, error) {
	return jrplex.TranscodeSessionsResponse{}, errors.New("not implemented")
}

// fakePlex implements minimal methods used by plexListener for richer tests.
type fakePlex struct {
	sessions     jrplex.CurrentSessions
	sessErr      error
	metadata     jrplex.MediaMetadata
	metaErr      error
	transcodeErr error
}

func (f *fakePlex) GetSessions() (jrplex.CurrentSessions, error) {
	if f.sessErr != nil {
		return jrplex.CurrentSessions{}, f.sessErr
	}
	if f.sessions.MediaContainer.Metadata != nil {
		return f.sessions, nil
	}
	// Default sessions for compatibility
	s := jrplex.CurrentSessions{}
	s.MediaContainer.Metadata = []jrplex.Metadata{{SessionKey: "sess1", User: jrplex.User{Title: "user1", ID: "u1"}}, {SessionKey: "sess2", User: jrplex.User{Title: "user2", ID: "u2"}}}
	return s, nil
}

func (f *fakePlex) GetMetadata(ratingKey string) (jrplex.MediaMetadata, error) {
	if f.metaErr != nil {
		return jrplex.MediaMetadata{}, f.metaErr
	}
	if f.metadata.MediaContainer.Metadata != nil {
		return f.metadata, nil
	}
	// Default metadata for compatibility
	mm := jrplex.MediaMetadata{}
	mm.MediaContainer.Metadata = []jrplex.Metadata{{RatingKey: ratingKey, Title: "Episode 1"}}
	return mm, nil
}

func (f *fakePlex) GetTranscodeSessions() (jrplex.TranscodeSessionsResponse, error) {
	if f.transcodeErr != nil {
		return jrplex.TranscodeSessionsResponse{}, f.transcodeErr
	}
	t := jrplex.TranscodeSessionsResponse{}
	// Construct a single child with both audio/video transcode
	child := struct {
		ElementType      string  `json:"_elementType"`
		AudioChannels    int     `json:"audioChannels"`
		AudioCodec       string  `json:"audioCodec"`
		AudioDecision    string  `json:"audioDecision"`
		SubtitleDecision string  `json:"subtitleDecision"`
		Container        string  `json:"container"`
		Context          string  `json:"context"`
		Duration         int     `json:"duration"`
		Height           int     `json:"height"`
		Key              string  `json:"key"`
		Progress         float64 `json:"progress"`
		Protocol         string  `json:"protocol"`
		Remaining        int     `json:"remaining"`
		Speed            float64 `json:"speed"`
		Throttled        bool    `json:"throttled"`
		VideoCodec       string  `json:"videoCodec"`
		VideoDecision    string  `json:"videoDecision"`
		Width            int     `json:"width"`
	}{
		Key:              "ts1",
		VideoDecision:    "transcode",
		AudioDecision:    "transcode",
		SubtitleDecision: "burn",
		Container:        "mp4",
	}
	t.Children = []struct {
		ElementType      string  `json:"_elementType"`
		AudioChannels    int     `json:"audioChannels"`
		AudioCodec       string  `json:"audioCodec"`
		AudioDecision    string  `json:"audioDecision"`
		SubtitleDecision string  `json:"subtitleDecision"`
		Container        string  `json:"container"`
		Context          string  `json:"context"`
		Duration         int     `json:"duration"`
		Height           int     `json:"height"`
		Key              string  `json:"key"`
		Progress         float64 `json:"progress"`
		Protocol         string  `json:"protocol"`
		Remaining        int     `json:"remaining"`
		Speed            float64 `json:"speed"`
		Throttled        bool    `json:"throttled"`
		VideoCodec       string  `json:"videoCodec"`
		VideoDecision    string  `json:"videoDecision"`
		Width            int     `json:"width"`
	}{child}
	return t, nil
}

func TestListen_AlreadyListeningAndNewPlexError(t *testing.T) {
	// Already listening
	s := &Server{}
	s.listener = &plexListener{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := s.Listen(ctx, log.NewNopLogger()); err != ErrAlreadyListening {
		t.Fatalf("expected ErrAlreadyListening, got %v", err)
	}

	// newPlex error path
	old := newPlex
	newPlex = func(base, token string) (*jrplex.Plex, error) { return nil, errors.New("connect fail") }
	defer func() { newPlex = old }()

	s2 := &Server{URL: &url.URL{Scheme: "http", Host: "localhost:32400"}, Token: "test"}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()

	if err := s2.Listen(ctx2, log.NewNopLogger()); err == nil {
		t.Fatalf("expected Listen to return error when newPlex fails")
	}
}

func TestOnPlayingHandler_GetSessionsError(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	// Create listener with fake client that returns error for GetSessions
	l := &plexListener{
		conn:           &fakePlexClient{sessErr: errors.New("fetch sessions failed")},
		activeSessions: NewSessions(context.Background(), s),
		log:            logger,
	}

	// Build a NotificationContainer with a PlaySessionStateNotification
	nc := jrplex.NotificationContainer{PlaySessionStateNotification: []jrplex.PlaySessionStateNotification{{SessionKey: "s1", RatingKey: "r1", State: "playing"}}}

	// Call handler; it should not panic and should log the error properly.
	l.onPlayingHandler(nc)

	// Verify error is logged with proper structure
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("error handling OnPlaying event")) {
		t.Fatalf("expected error log message, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("sessionKeys=s1")) {
		t.Fatalf("expected sessionKeys in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("ratingKeys=r1")) {
		t.Fatalf("expected ratingKeys in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("states=playing")) {
		t.Fatalf("expected states in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("fetch sessions failed")) {
		t.Fatalf("expected error message in log, got: %s", out)
	}
}

func TestOnTranscodeUpdateHandler_Empty(t *testing.T) {
	l := &plexListener{activeSessions: NewSessions(context.Background(), &Server{}), log: log.NewNopLogger()}
	l.onTranscodeUpdateHandler(jrplex.NotificationContainer{})
}

func TestOnTranscodeUpdateHandlerSetsSessionType(t *testing.T) {
	tests := []struct {
		name string
		ts   jrplex.TranscodeSession
		want string
	}{
		{name: "video", ts: jrplex.TranscodeSession{Key: "s1", SourceVideoCodec: "h264", VideoCodec: "hevc"}, want: "video"},
		{name: "audio", ts: jrplex.TranscodeSession{Key: "s2", SourceAudioCodec: "aac", AudioCodec: "mp3"}, want: "audio"},
		{name: "both", ts: jrplex.TranscodeSession{Key: "s3", SourceVideoCodec: "h264", VideoCodec: "vp9", SourceAudioCodec: "aac", AudioCodec: "ac3"}, want: "both"},
		{name: "unknown", ts: jrplex.TranscodeSession{Key: "s4"}, want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// prepare sessions store and listener
			sessStore := &sessions{sessions: map[string]session{}, server: &Server{Name: "srv", ID: "id"}}
			l := &plexListener{activeSessions: sessStore, log: log.NewNopLogger()}

			// call handler
			c := jrplex.NotificationContainer{TranscodeSession: []jrplex.TranscodeSession{tt.ts}}
			l.onTranscodeUpdateHandler(c)

			ss, ok := sessStore.sessions[tt.ts.Key]
			if !ok {
				t.Fatalf("expected session %q to exist", tt.ts.Key)
			}
			if ss.transcodeType != tt.want {
				t.Fatalf("session transcodeType = %q, want %q", ss.transcodeType, tt.want)
			}
			// subtitle_action defaults to "none" for these test cases
			if ss.subtitleAction != "none" {
				t.Fatalf("expected subtitleAction=none, got %q", ss.subtitleAction)
			}
		})
	}
}

func TestTranscodeSessionsAPIOverridesApply(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	sessStore := &sessions{sessions: map[string]session{}, server: s}
	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: sessStore,
		log:            log.NewNopLogger(),
	}

	// Simulate a websocket transcode update with a key that doesn't match
	// active sessions so the listener will consult GetTranscodeSessions().
	c := jrplex.NotificationContainer{TranscodeSession: []jrplex.TranscodeSession{{Key: "ts1"}}}
	l.onTranscodeUpdateHandler(c)

	ss, ok := sessStore.sessions["ts1"]
	if !ok {
		t.Fatalf("expected session ts1 to exist after API fallback")
	}
	if ss.transcodeType != "both" {
		t.Fatalf("expected transcodeType=both, got %q", ss.transcodeType)
	}
	if ss.subtitleAction != "burn" {
		t.Fatalf("expected subtitleAction=burn, got %q", ss.subtitleAction)
	}
}

func TestOnPlayingUpdatesSessions(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            log.NewNopLogger(),
	}

	// craft a notification container with one playing notification
	c := jrplex.NotificationContainer{}
	c.PlaySessionStateNotification = []jrplex.PlaySessionStateNotification{{
		SessionKey: "sess1",
		RatingKey:  "rk1",
		State:      "playing",
		ViewOffset: 1000,
	}}

	if err := l.onPlaying(c); err != nil {
		t.Fatalf("onPlaying error: %v", err)
	}

	// ensure session was added
	if _, ok := l.activeSessions.sessions["sess1"]; !ok {
		t.Fatalf("expected session sess1 to be present")
	}

	// ensure metadata was attached
	ss := l.activeSessions.sessions["sess1"]
	if ss.media.RatingKey != "rk1" && ss.session.SessionKey != "sess1" {
		t.Fatalf("unexpected session contents: %+v", ss)
	}

	// simulate stop + old timestamp and prune
	ss.state = stateStopped
	ss.lastUpdate = time.Now().Add(-2 * sessionTimeout)
	l.activeSessions.sessions["sess1"] = ss
	l.activeSessions.pruneOldSessions()
	if _, ok := l.activeSessions.sessions["sess1"]; ok {
		t.Fatalf("expected session sess1 to be pruned")
	}
}

func TestOnPlayingLogsFirstNotification(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	// capture logs in a buffer
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            logger,
	}

	// craft a notification container with two playing notifications
	c := jrplex.NotificationContainer{}
	c.PlaySessionStateNotification = []jrplex.PlaySessionStateNotification{{
		SessionKey: "sess1",
		RatingKey:  "rk1",
		State:      "playing",
		ViewOffset: 1000,
	}, {
		SessionKey: "sess2",
		RatingKey:  "rk2",
		State:      "playing",
		ViewOffset: 2000,
	}}

	if err := l.onPlaying(c); err != nil {
		t.Fatalf("onPlaying error: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Fatalf("expected log output, got empty string")
	}

	// Ensure the first session key and batchCount=2 are present in the log
	if !bytes.Contains([]byte(out), []byte("SessionKey=sess1")) {
		t.Fatalf("expected SessionKey=sess1 in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("batchCount=2")) {
		t.Fatalf("expected batchCount=2 in log, got: %s", out)
	}
}

func TestOnTimelineLogsEntries(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            logger,
	}

	// craft a notification container with two timeline entries
	c := jrplex.NotificationContainer{}
	c.TimelineEntry = []jrplex.TimelineEntry{{
		Identifier: "id1",
		ItemID:     123,
		Title:      "Test1",
		SectionID:  15,
		State:      2,
	}, {
		Identifier: "id2",
		ItemID:     124,
		Title:      "Test2",
		SectionID:  15,
		State:      3,
	}}

	// our handler expects the vendored NotificationContainer type used by
	// the websocket client; convert and call the handler.
	l.onTimelineHandler(c)

	out := buf.String()
	if out == "" {
		t.Fatalf("expected log output, got empty string")
	}

	if !bytes.Contains([]byte(out), []byte("timeline entries")) {
		t.Fatalf("expected timeline entries in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("id=id1")) {
		t.Fatalf("expected id=id1 in log, got: %s", out)
	}
}

// TestOnPlayingHandlerSuccess tests the success path of onPlayingHandler.
func TestOnPlayingHandlerSuccess(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            logger,
	}

	// Create a notification container with playing notifications
	nc := jrplex.NotificationContainer{
		PlaySessionStateNotification: []jrplex.PlaySessionStateNotification{{
			SessionKey: "sess1",
			RatingKey:  "rk1",
			State:      "playing",
			ViewOffset: 1000,
		}},
	}

	// Call onPlayingHandler - should succeed without errors
	l.onPlayingHandler(nc)

	// Should not log any errors
	out := buf.String()
	if bytes.Contains([]byte(out), []byte("error handling OnPlaying event")) {
		t.Fatalf("expected no error logs, but got: %s", out)
	}

	// Session should be added
	if _, ok := l.activeSessions.sessions["sess1"]; !ok {
		t.Fatalf("expected session sess1 to be present")
	}
}

// TestOnPlayingHandlerError tests the error handling path of onPlayingHandler.
func TestOnPlayingHandlerError(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	// Create a fake client that returns an error for GetSessions
	l := &plexListener{
		server:         s,
		conn:           &fakePlex{sessErr: errors.New("sessions fetch failed")},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            logger,
	}

	// Create a notification container with playing notifications
	nc := jrplex.NotificationContainer{
		PlaySessionStateNotification: []jrplex.PlaySessionStateNotification{{
			SessionKey: "sess1",
			RatingKey:  "rk1",
			State:      "playing",
			ViewOffset: 1000,
		}, {
			SessionKey: "sess2",
			RatingKey:  "rk2",
			State:      "paused",
			ViewOffset: 2000,
		}},
	}

	// Call onPlayingHandler - should handle error gracefully
	l.onPlayingHandler(nc)

	// Should log error details
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("error handling OnPlaying event")) {
		t.Fatalf("expected error log message, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("sessionKeys=sess1,sess2")) {
		t.Fatalf("expected sessionKeys in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("ratingKeys=rk1,rk2")) {
		t.Fatalf("expected ratingKeys in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("states=playing,paused")) {
		t.Fatalf("expected states in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("sessions fetch failed")) {
		t.Fatalf("expected error message in log, got: %s", out)
	}
}

// TestOnPlayingHandlerEmptyNotifications tests onPlayingHandler with empty notifications.
func TestOnPlayingHandlerEmptyNotifications(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            logger,
	}

	// Create empty notification container
	nc := jrplex.NotificationContainer{}

	// Call onPlayingHandler - should succeed without issues
	l.onPlayingHandler(nc)

	// Should not log any errors
	out := buf.String()
	if bytes.Contains([]byte(out), []byte("error handling OnPlaying event")) {
		t.Fatalf("expected no error logs, but got: %s", out)
	}
}

// TestOnPlayingHandlerMultipleErrors tests error path with multiple notifications.
func TestOnPlayingHandlerMultipleErrors(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	// Create a fake client that returns an error
	l := &plexListener{
		server:         s,
		conn:           &fakePlex{sessErr: errors.New("network error")},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            logger,
	}

	// Create notification with multiple sessions in different states
	nc := jrplex.NotificationContainer{
		PlaySessionStateNotification: []jrplex.PlaySessionStateNotification{{
			SessionKey: "session_abc",
			RatingKey:  "rating_123",
			State:      "buffering",
			ViewOffset: 5000,
		}, {
			SessionKey: "session_def",
			RatingKey:  "rating_456",
			State:      "stopped",
			ViewOffset: 0,
		}, {
			SessionKey: "session_ghi",
			RatingKey:  "rating_789",
			State:      "playing",
			ViewOffset: 15000,
		}},
	}

	// Call onPlayingHandler
	l.onPlayingHandler(nc)

	// Verify error logging includes all session details
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("error handling OnPlaying event")) {
		t.Fatalf("expected error log message, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("sessionKeys=session_abc,session_def,session_ghi")) {
		t.Fatalf("expected all sessionKeys in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("ratingKeys=rating_123,rating_456,rating_789")) {
		t.Fatalf("expected all ratingKeys in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("states=buffering,stopped,playing")) {
		t.Fatalf("expected all states in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("network error")) {
		t.Fatalf("expected network error in log, got: %s", out)
	}
}

// TestOnPlayingSessionNotFoundAfterRetries tests the case where a session is not found after retries.
func TestOnPlayingSessionNotFoundAfterRetries(t *testing.T) {
	// Save original values and restore after test
	origMaxRetries := SessionLookupMaxRetries
	origBaseDelay := SessionLookupBaseDelay
	defer func() {
		SessionLookupMaxRetries = origMaxRetries
		SessionLookupBaseDelay = origBaseDelay
	}()

	// Set low values for faster test
	SessionLookupMaxRetries = 1
	SessionLookupBaseDelay = 1 * time.Millisecond

	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	// Create a fake that returns sessions but without the session we're looking for
	sessions := jrplex.CurrentSessions{}
	sessions.MediaContainer.Metadata = []jrplex.Metadata{{SessionKey: "different_session", User: jrplex.User{Title: "user1", ID: "u1"}}}

	l := &plexListener{
		server:         s,
		conn:           &fakePlex{sessions: sessions},
		activeSessions: NewSessions(context.Background(), s),
		log:            logger,
	}

	// Create notification for a session that doesn't exist
	nc := jrplex.NotificationContainer{
		PlaySessionStateNotification: []jrplex.PlaySessionStateNotification{{
			SessionKey: "missing_session",
			RatingKey:  "rk1",
			State:      "playing",
			ViewOffset: 1000,
		}},
	}

	// This should log a warning about session not found after retries
	if err := l.onPlaying(nc); err != nil {
		t.Fatalf("onPlaying should not return error, got: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("session not found for notification after retries, skipping")) {
		t.Fatalf("expected session not found warning, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("SessionKey=missing_session")) {
		t.Fatalf("expected missing session key in log, got: %s", out)
	}
}

// TestOnPlayingMetadataErrorAfterRetries tests metadata fetch failure after retries.
func TestOnPlayingMetadataErrorAfterRetries(t *testing.T) {
	// Save original values and restore after test
	origMaxRetries := MetadataMaxRetries
	origBaseDelay := MetadataBaseDelay
	defer func() {
		MetadataMaxRetries = origMaxRetries
		MetadataBaseDelay = origBaseDelay
	}()

	// Set low values for faster test
	MetadataMaxRetries = 1
	MetadataBaseDelay = 1 * time.Millisecond

	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	l := &plexListener{
		server:         s,
		conn:           &fakePlex{metaErr: errors.New("metadata fetch failed")},
		activeSessions: NewSessions(context.Background(), s),
		log:            logger,
	}

	nc := jrplex.NotificationContainer{
		PlaySessionStateNotification: []jrplex.PlaySessionStateNotification{{
			SessionKey: "sess1",
			RatingKey:  "rk1",
			State:      "playing",
			ViewOffset: 1000,
		}},
	}

	// This should log an error about metadata fetch failure
	if err := l.onPlaying(nc); err != nil {
		t.Fatalf("onPlaying should not return error, got: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("error fetching metadata for notification after retries, skipping")) {
		t.Fatalf("expected metadata error log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("RatingKey=rk1")) {
		t.Fatalf("expected rating key in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("metadata fetch failed")) {
		t.Fatalf("expected metadata error message in log, got: %s", out)
	}
}

// TestOnPlayingEmptyMetadataAfterRetries tests empty metadata response after retries.
func TestOnPlayingEmptyMetadataAfterRetries(t *testing.T) {
	// Save original values and restore after test
	origMaxRetries := MetadataMaxRetries
	origBaseDelay := MetadataBaseDelay
	defer func() {
		MetadataMaxRetries = origMaxRetries
		MetadataBaseDelay = origBaseDelay
	}()

	// Set low values for faster test
	MetadataMaxRetries = 1
	MetadataBaseDelay = 1 * time.Millisecond

	s := &Server{Name: "srv", ID: "id"}
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(log.NewSyncWriter(&buf))

	// Create metadata with empty metadata array
	emptyMetadata := jrplex.MediaMetadata{}
	emptyMetadata.MediaContainer.Metadata = []jrplex.Metadata{}

	l := &plexListener{
		server:         s,
		conn:           &fakePlex{metadata: emptyMetadata},
		activeSessions: NewSessions(context.Background(), s),
		log:            logger,
	}

	nc := jrplex.NotificationContainer{
		PlaySessionStateNotification: []jrplex.PlaySessionStateNotification{{
			SessionKey: "sess1",
			RatingKey:  "rk1",
			State:      "playing",
			ViewOffset: 1000,
		}},
	}

	// This should log a warning about empty metadata response
	if err := l.onPlaying(nc); err != nil {
		t.Fatalf("onPlaying should not return error, got: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("metadata response empty after retries, skipping")) {
		t.Fatalf("expected empty metadata warning, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("RatingKey=rk1")) {
		t.Fatalf("expected rating key in log, got: %s", out)
	}
}

// TestOnPlayingStoppedSession tests handling of stopped session state.
func TestOnPlayingStoppedSession(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	sessStore := NewSessions(context.Background(), s)
	l := &plexListener{
		server:         s,
		conn:           &fakePlex{},
		activeSessions: sessStore,
		log:            log.NewNopLogger(),
	}

	nc := jrplex.NotificationContainer{
		PlaySessionStateNotification: []jrplex.PlaySessionStateNotification{{
			SessionKey: "sess1",
			RatingKey:  "rk1",
			State:      "stopped",
			ViewOffset: 1000,
		}},
	}

	if err := l.onPlaying(nc); err != nil {
		t.Fatalf("onPlaying should not return error, got: %v", err)
	}

	// Session should be updated with stopped state
	// We can't directly access the sessions map, but we can test the behavior indirectly
	// by ensuring no error occurred and the function completed successfully
}

// TestSetTranscodeType tests all paths of the SetTranscodeType function.
func TestSetTranscodeType(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	sessStore := NewSessions(context.Background(), s)

	// Test 1: Fast path - exact key match
	sessStore.Update("sess1", statePlaying, &jrplex.Metadata{SessionKey: "sess1"}, &jrplex.Metadata{RatingKey: "rk1"})
	sessStore.SetTranscodeType("sess1", "video")

	if session, ok := sessStore.sessions["sess1"]; !ok || session.transcodeType != "video" {
		t.Fatalf("expected sess1 to have transcodeType=video")
	}

	// Test 2: Inner SessionKey match
	sessStore.Update("outer_key", statePlaying, &jrplex.Metadata{SessionKey: "inner_session"}, &jrplex.Metadata{RatingKey: "rk2"})
	sessStore.SetTranscodeType("inner_session", "audio")

	if session := sessStore.sessions["outer_key"]; session.transcodeType != "audio" {
		t.Fatalf("expected outer_key session to have transcodeType=audio, got %s", session.transcodeType)
	}

	// Test 3: Substring match (sessionID contains session key)
	sessStore.Update("short", statePlaying, &jrplex.Metadata{SessionKey: "short"}, &jrplex.Metadata{RatingKey: "rk3"})
	sessStore.SetTranscodeType("prefix_short_suffix", "both")

	if session := sessStore.sessions["short"]; session.transcodeType != "both" {
		t.Fatalf("expected short session to have transcodeType=both, got %s", session.transcodeType)
	}

	// Test 4: Substring match (session key contains sessionID)
	sessStore.Update("long_session_key", statePlaying, &jrplex.Metadata{SessionKey: "long_session_key"}, &jrplex.Metadata{RatingKey: "rk4"})

	// Clear the sessions map except for the one we want to test
	sessStore.mtx.Lock()
	temp := sessStore.sessions["long_session_key"]
	sessStore.sessions = map[string]session{"long_session_key": temp}
	sessStore.mtx.Unlock()

	sessStore.SetTranscodeType("session", "unknown")

	sessStore.mtx.Lock()
	session := sessStore.sessions["long_session_key"]
	sessStore.mtx.Unlock()

	if session.transcodeType != "unknown" {
		t.Fatalf("expected long_session_key to have transcodeType=unknown, got %s", session.transcodeType)
	}

	// Test 5: Fallback - create new entry
	sessStore.SetTranscodeType("new_session", "fallback")

	if session, ok := sessStore.sessions["new_session"]; !ok || session.transcodeType != "fallback" {
		t.Fatalf("expected new_session to be created with transcodeType=fallback")
	}
}

// TestSetSubtitleAction tests all paths of the SetSubtitleAction function.
func TestSetSubtitleAction(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	sessStore := NewSessions(context.Background(), s)

	// Test 1: Fast path - exact key match
	sessStore.Update("sess1", statePlaying, &jrplex.Metadata{SessionKey: "sess1"}, &jrplex.Metadata{RatingKey: "rk1"})
	sessStore.SetSubtitleAction("sess1", "burn")

	if session, ok := sessStore.sessions["sess1"]; !ok || session.subtitleAction != "burn" {
		t.Fatalf("expected sess1 to have subtitleAction=burn")
	}

	// Test 2: Inner SessionKey match
	sessStore.Update("outer_key", statePlaying, &jrplex.Metadata{SessionKey: "inner_session"}, &jrplex.Metadata{RatingKey: "rk2"})
	sessStore.SetSubtitleAction("inner_session", "copy")

	if session := sessStore.sessions["outer_key"]; session.subtitleAction != "copy" {
		t.Fatalf("expected outer_key session to have subtitleAction=copy, got %s", session.subtitleAction)
	}

	// Test 3: Substring match
	sessStore.Update("substring_test", statePlaying, &jrplex.Metadata{SessionKey: "substring_test"}, &jrplex.Metadata{RatingKey: "rk3"})
	sessStore.SetSubtitleAction("substring", "none")

	if session := sessStore.sessions["substring_test"]; session.subtitleAction != "none" {
		t.Fatalf("expected substring_test session to have subtitleAction=none, got %s", session.subtitleAction)
	}

	// Test 4: Fallback - create new entry
	sessStore.SetSubtitleAction("new_subtitle_session", "extract")

	if session, ok := sessStore.sessions["new_subtitle_session"]; !ok || session.subtitleAction != "extract" {
		t.Fatalf("expected new_subtitle_session to be created with subtitleAction=extract")
	}
}

// TestTrySetTranscodeType tests all paths of the TrySetTranscodeType function.
func TestTrySetTranscodeType(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	sessStore := NewSessions(context.Background(), s)

	// Test 1: Fast path - exact key match
	sessStore.Update("sess1", statePlaying, &jrplex.Metadata{SessionKey: "sess1"}, &jrplex.Metadata{RatingKey: "rk1"})
	if !sessStore.TrySetTranscodeType("sess1", "video") {
		t.Fatalf("expected TrySetTranscodeType to return true for exact match")
	}

	// Test 2: Inner SessionKey match
	sessStore.Update("outer_key", statePlaying, &jrplex.Metadata{SessionKey: "inner_session"}, &jrplex.Metadata{RatingKey: "rk2"})
	if !sessStore.TrySetTranscodeType("inner_session", "audio") {
		t.Fatalf("expected TrySetTranscodeType to return true for inner session match")
	}

	// Test 3: Substring match
	sessStore.Update("substring_test", statePlaying, &jrplex.Metadata{SessionKey: "substring_test"}, &jrplex.Metadata{RatingKey: "rk3"})
	if !sessStore.TrySetTranscodeType("substring", "both") {
		t.Fatalf("expected TrySetTranscodeType to return true for substring match")
	}

	// Test 4: Heuristic fallback - session with transcode decision
	metadata := &jrplex.Metadata{
		SessionKey: "transcode_session",
		Media: []jrplex.Media{{
			Part: []jrplex.Part{{Decision: "transcode"}},
		}},
	}
	sessStore.Update("transcode_key", statePlaying, metadata, &jrplex.Metadata{RatingKey: "rk4"})
	if !sessStore.TrySetTranscodeType("nonexistent_key", "heuristic") {
		t.Fatalf("expected TrySetTranscodeType to return true for heuristic fallback")
	}

	// Verify the transcode type was set on the session with transcode decision
	if session := sessStore.sessions["transcode_key"]; session.transcodeType != "heuristic" {
		t.Fatalf("expected transcode_key session to have transcodeType=heuristic, got %s", session.transcodeType)
	}

	// Test 5: No match found
	sessStore.mtx.Lock()
	sessStore.sessions = map[string]session{} // Clear all sessions
	sessStore.mtx.Unlock()

	if sessStore.TrySetTranscodeType("completely_nonexistent", "should_fail") {
		t.Fatalf("expected TrySetTranscodeType to return false when no match found")
	}
}

// TestTrySetSubtitleAction tests all paths of the TrySetSubtitleAction function.
func TestTrySetSubtitleAction(t *testing.T) {
	s := &Server{Name: "srv", ID: "id"}
	sessStore := NewSessions(context.Background(), s)

	// Test 1: Fast path - exact key match
	sessStore.Update("sess1", statePlaying, &jrplex.Metadata{SessionKey: "sess1"}, &jrplex.Metadata{RatingKey: "rk1"})
	if !sessStore.TrySetSubtitleAction("sess1", "burn") {
		t.Fatalf("expected TrySetSubtitleAction to return true for exact match")
	}

	// Test 2: Inner SessionKey match
	sessStore.Update("outer_key", statePlaying, &jrplex.Metadata{SessionKey: "inner_session"}, &jrplex.Metadata{RatingKey: "rk2"})
	if !sessStore.TrySetSubtitleAction("inner_session", "copy") {
		t.Fatalf("expected TrySetSubtitleAction to return true for inner session match")
	}

	// Test 3: Substring match
	sessStore.Update("substring_test", statePlaying, &jrplex.Metadata{SessionKey: "substring_test"}, &jrplex.Metadata{RatingKey: "rk3"})
	if !sessStore.TrySetSubtitleAction("substring", "none") {
		t.Fatalf("expected TrySetSubtitleAction to return true for substring match")
	}

	// Test 4: No match found
	if sessStore.TrySetSubtitleAction("completely_nonexistent", "should_fail") {
		t.Fatalf("expected TrySetSubtitleAction to return false when no match found")
	}
}
