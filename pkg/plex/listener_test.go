package plex

import (
	"bytes"
	"testing"
	"time"

	"github.com/go-kit/log"
	jrplex "github.com/jrudio/go-plex-client"
)

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

// fakePlex implements minimal methods used by plexListener
type fakePlex struct{}

func (f *fakePlex) GetSessions() (jrplex.CurrentSessions, error) {
	s := jrplex.CurrentSessions{}
	s.MediaContainer.Metadata = []jrplex.Metadata{{
		SessionKey: "sess1",
		User:       jrplex.User{Title: "user1", ID: "u1"},
	}, {
		SessionKey: "sess2",
		User:       jrplex.User{Title: "user2", ID: "u2"},
	}}
	return s, nil
}

func (f *fakePlex) GetMetadata(ratingKey string) (jrplex.MediaMetadata, error) {
	mm := jrplex.MediaMetadata{}
	mm.MediaContainer.Metadata = []jrplex.Metadata{{
		RatingKey: ratingKey,
		Title:     "Episode 1",
	}}
	return mm, nil
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
