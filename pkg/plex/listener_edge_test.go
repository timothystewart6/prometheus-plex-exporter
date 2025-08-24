package plex

// NOTE: Test fixtures in this file use randomized/sanitized session keys and
// related identifiers to keep tests realistic but non-identifying.

import (
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	ttPlex "github.com/timothystewart6/go-plex-client"
)

// Test handler when activeSessions is nil — should not panic and should be no-op.
func TestOnTranscodeUpdateHandlerNilActiveSessions(t *testing.T) {
	l := &plexListener{activeSessions: nil, log: log.NewNopLogger()}
	c := ttPlex.NotificationContainer{TranscodeSession: []ttPlex.TranscodeSession{{Key: "noactive", VideoCodec: "hevc", SourceVideoCodec: "h264"}}}
	l.onTranscodeUpdateHandler(c) // must not panic
}

// Test that handler creates/sets a session transcodeType even when session didn't exist.
func TestOnTranscodeUpdateHandlerCreatesSession(t *testing.T) {
	sessStore := &sessions{sessions: map[string]session{}, server: &Server{Name: "srv", ID: "id"}}
	l := &plexListener{activeSessions: sessStore, log: log.NewNopLogger()}

	ts := ttPlex.TranscodeSession{Key: "s-create", SourceAudioCodec: "aac", AudioCodec: "mp3"}
	c := ttPlex.NotificationContainer{TranscodeSession: []ttPlex.TranscodeSession{ts}}
	l.onTranscodeUpdateHandler(c)

	ss, ok := sessStore.sessions[ts.Key]
	if !ok {
		t.Fatalf("expected session %q to exist", ts.Key)
	}
	if ss.transcodeType != "audio" {
		t.Fatalf("expected transcodeType=audio, got %q", ss.transcodeType)
	}
	if ss.subtitleAction != "none" {
		t.Fatalf("expected subtitleAction=none, got %q", ss.subtitleAction)
	}
}

// Concurrent updates should not cause races; final value should be one of expected.
func TestOnTranscodeUpdateHandlerConcurrent(t *testing.T) {
	sessStore := &sessions{sessions: map[string]session{}, server: &Server{Name: "srv", ID: "id"}}
	l := &plexListener{activeSessions: sessStore, log: log.NewNopLogger()}

	var wg sync.WaitGroup
	kinds := []ttPlex.TranscodeSession{
		{Key: "concurrent", SourceVideoCodec: "h264", VideoCodec: "vp9"},
		{Key: "concurrent", SourceAudioCodec: "aac", AudioCodec: "mp3"},
		{Key: "concurrent", SourceVideoCodec: "h264", VideoCodec: "hevc", SourceAudioCodec: "aac", AudioCodec: "ac3"},
		{Key: "concurrent"},
	}

	for i := 0; i < 50; i++ {
		for _, k := range kinds {
			wg.Add(1)
			go func(ts ttPlex.TranscodeSession) {
				defer wg.Done()
				c := ttPlex.NotificationContainer{TranscodeSession: []ttPlex.TranscodeSession{ts}}
				l.onTranscodeUpdateHandler(c)
			}(k)
		}
	}
	wg.Wait()

	ss, ok := sessStore.sessions["concurrent"]
	if !ok {
		t.Fatalf("expected session 'concurrent' to exist")
	}
	switch ss.transcodeType {
	case "audio", "video", "both", "unknown":
		// ok
	default:
		t.Fatalf("unexpected transcodeType %q", ss.transcodeType)
	}
	// subtitleAction should be one of these (or none)
	switch ss.subtitleAction {
	case "none", "copy", "burn":
		// ok
	default:
		t.Fatalf("unexpected subtitleAction %q", ss.subtitleAction)
	}
}

// fakeRetry simulates GetSessions returning empty first, then returning the session.
type fakeRetry struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeRetry) GetSessions() (ttPlex.CurrentSessions, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == 0 {
		f.calls++
		return ttPlex.CurrentSessions{}, nil
	}
	// return a session matching sess1
	s := ttPlex.CurrentSessions{}
	s.MediaContainer.Metadata = []ttPlex.Metadata{{SessionKey: "sess1", User: ttPlex.User{Title: "u1", ID: "id1"}}}
	return s, nil
}

func (f *fakeRetry) GetMetadata(ratingKey string) (ttPlex.MediaMetadata, error) {
	mm := ttPlex.MediaMetadata{}
	mm.MediaContainer.Metadata = []ttPlex.Metadata{{RatingKey: ratingKey, Title: "E1"}}
	return mm, nil
}

func (f *fakeRetry) GetTranscodeSessions() (ttPlex.TranscodeSessionsResponse, error) {
	return ttPlex.TranscodeSessionsResponse{}, nil
}

func TestOnPlayingRetriesGetSessions(t *testing.T) {
	// speed up retries for test
	oldMax := SessionLookupMaxRetries
	oldBase := SessionLookupBaseDelay
	SessionLookupMaxRetries = 3
	SessionLookupBaseDelay = 1 * time.Millisecond
	defer func() { SessionLookupMaxRetries = oldMax; SessionLookupBaseDelay = oldBase }()

	s := &Server{Name: "srv", ID: "id"}
	l := &plexListener{
		server:         s,
		conn:           &fakeRetry{},
		activeSessions: &sessions{sessions: map[string]session{}, server: s},
		log:            log.NewNopLogger(),
	}

	c := ttPlex.NotificationContainer{}
	c.PlaySessionStateNotification = []ttPlex.PlaySessionStateNotification{{SessionKey: "sess1", RatingKey: "rk1", State: "playing", ViewOffset: 0}}

	if err := l.onPlaying(c); err != nil {
		t.Fatalf("onPlaying failed: %v", err)
	}

	if _, ok := l.activeSessions.sessions["sess1"]; !ok {
		t.Fatalf("expected session sess1 to be present after retries")
	}
}
