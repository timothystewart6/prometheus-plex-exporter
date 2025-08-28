package plex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/websocket"
	"github.com/timothystewart6/go-plex-client"
)

var (
	ErrAlreadyListening = errors.New("already listening")
)

// Retry/backoff tuning variables. These are exported so callers (for
// example `cmd/prometheus-plex-exporter/main.go` or tests) can adjust them
// at runtime if you observe races with your Plex server.
var (
	// SessionLookupMaxRetries controls how many times we retry fetching the
	// current sessions when a notification refers to a session not present
	// in the initial listing.
	SessionLookupMaxRetries = 3

	// SessionLookupBaseDelay is the base delay for session lookup retries; the
	// delay doubles each attempt (exponential backoff).
	SessionLookupBaseDelay = 100 * time.Millisecond

	// MetadataMaxRetries controls how many times we retry fetching metadata
	// for a rating key when it's not immediately available.
	MetadataMaxRetries = 3

	// MetadataBaseDelay is the base delay for metadata fetch retries.
	MetadataBaseDelay = 100 * time.Millisecond
)

// newPlex is a variable so tests can replace it with a fake constructor.
// Keep the wrapper with the old two-argument signature so tests that assign
// a func(base, token string) (*ttPlex.Plex, error) remain compatible.
var newPlex = func(base, token string) (*plex.Plex, error) { return plex.New(base, token) }

type plexClient interface {
	GetSessions() (plex.CurrentSessions, error)
	GetMetadata(string) (plex.MediaMetadata, error)
	GetTranscodeSessions() (plex.TranscodeSessionsResponse, error)
}

// Ensure the concrete plex.Plex from the vendored client implements the
// minimal interface we require. This provides a helpful compile-time check
// and documents the intent: production code uses *plex.Plex while tests may
// provide fakes that implement the same methods.
var _ plexClient = (*plex.Plex)(nil)

type plexListener struct {
	server         *Server
	conn           plexClient
	activeSessions *sessions
	log            log.Logger
}

func (s *Server) Listen(ctx context.Context, log log.Logger) error {
	s.mtx.Lock()
	if s.listener != nil {
		s.mtx.Unlock()
		return ErrAlreadyListening
	}

	conn, err := newPlex(s.URL.String(), s.Token)
	if err != nil {
		s.mtx.Unlock()
		return fmt.Errorf("failed to connect to %s: %w", s.URL.String(), err)
	}

	s.listener = &plexListener{
		server:         s,
		conn:           conn,
		activeSessions: NewSessions(ctx, s),
		log:            log,
	}

	s.mtx.Unlock()

	// forward context completion to timothystewart6/go-plex-client
	ctrlC := make(chan os.Signal, 1)
	go func() {
		<-ctx.Done()
		close(ctrlC)
	}()

	doneChan := make(chan error, 1)
	var closeOnce sync.Once
	var errSent bool
	var mutex sync.Mutex

	onError := func(err error) {
		mutex.Lock()
		defer mutex.Unlock()

		// If we've already sent an error or closed, ignore subsequent calls
		if errSent {
			return
		}

		var closeErr *websocket.CloseError
		if errors.As(err, &closeErr) {
			if closeErr.Code == websocket.CloseNormalClosure {
				errSent = true
				closeOnce.Do(func() { close(doneChan) })
				return
			}
		}
		_ = level.Error(log).Log("msg", "error in websocket processing", "err", err)

		// Try to send error, but don't panic if channel is closed
		select {
		case doneChan <- err:
			errSent = true
		default:
			// Channel already closed or full, ignore
		}
		closeOnce.Do(func() { close(doneChan) })
	}

	events := plex.NewNotificationEvents()
	events.OnPlaying(s.listener.onPlayingHandler)
	events.OnTimeline(s.listener.onTimelineHandler)
	// register transcode update handler to record transcode type
	events.OnTranscodeUpdate(s.listener.onTranscodeUpdateHandler)

	// TODO - Does this automatically reconnect on websocket failure?
	conn.SubscribeToNotifications(events, ctrlC, onError)
	select { // SubscribeToNotifications doesn't return error directly, so we read one from channel without blocking.
	case err = <-doneChan:
		return err
	default:
		// noop
	}

	_ = level.Info(log).Log("msg", "Successfully connected", "machineID", s.ID, "server", s.Name)

	return <-doneChan
}

func getSessionByID(sessions plex.CurrentSessions, sessionID string) *plex.Metadata {
	for _, session := range sessions.MediaContainer.Metadata {
		if sessionID == session.SessionKey {
			return &session
		}
	}
	return nil
}

func (l *plexListener) onPlayingHandler(c plex.NotificationContainer) {
	err := l.onPlaying(c)
	if err != nil {
		// Extract simple fields for structured logging so the logfmt encoder
		// doesn't attempt to serialize complex nested structs.
		var sessionKeys []string
		var ratingKeys []string
		var states []string
		for _, n := range c.PlaySessionStateNotification {
			sessionKeys = append(sessionKeys, n.SessionKey)
			ratingKeys = append(ratingKeys, n.RatingKey)
			states = append(states, n.State)
		}

		_ = level.Error(l.log).Log(
			"msg", "error handling OnPlaying event",
			"sessionKeys", strings.Join(sessionKeys, ","),
			"ratingKeys", strings.Join(ratingKeys, ","),
			"states", strings.Join(states, ","),
			"err", err,
		)
	}
}

// onTimelineHandler logs concise timeline entries when a 'timeline' event is received.
func (l *plexListener) onTimelineHandler(c plex.NotificationContainer) {
	// Log a summary of timeline entries without passing complex structs to the
	// log encoder. Include identifier, itemID, title, sectionID, and state.
	if len(c.TimelineEntry) == 0 {
		return
	}

	var summaries []string
	for _, te := range c.TimelineEntry {
		summaries = append(summaries, fmt.Sprintf("id=%s item=%d title=%s section=%d state=%d", te.Identifier, te.ItemID, te.Title, te.SectionID, te.State))
	}

	_ = level.Info(l.log).Log("msg", "timeline entries", "count", len(c.TimelineEntry), "entries", strings.Join(summaries, " | "))
}

// onTranscodeUpdateHandler receives TranscodeSession updates and logs a concise
// transcode type (audio/video/both). We avoid sending nested structs to the
// logger and only emit primitive fields.
//
// Note: example keys and fixture data shown in tests and examples are
// randomized/sanitized values that resemble real Plex identifiers but do not
// contain customer-identifying information. Tests and documentation intentionally
// use synthetic keys that are similar in shape to real data to exercise
// matching logic without exposing production identifiers.
// nolint:gocyclo // complexity tolerated for now; can be refactored later
func (l *plexListener) onTranscodeUpdateHandler(c plex.NotificationContainer) {
	if len(c.TranscodeSession) == 0 {
		return
	}

	for _, ts := range c.TranscodeSession {
		kind := transcodeKind(ts)

		// determine subtitle action: prefer explicit subtitleDecision when present
		subtitle := "none"
		sd := strings.ToLower(strings.TrimSpace(ts.SubtitleDecision))
		if sd != "" {
			switch sd {
			case "burn", "burn-in":
				subtitle = "burn"
			case "copy", "copying":
				subtitle = "copy"
			case "transcode", "transcoding":
				// Plex may report subtitleDecision as "transcode" when the
				// subtitle track is being converted/transcoded during playback.
				// Preserve this as its own action so metrics can distinguish
				// explicit transcode behavior from an actual burn.
				subtitle = "transcode"
			}
		} else {
			ctn := strings.ToLower(strings.TrimSpace(ts.Container))
			vDecision := strings.ToLower(strings.TrimSpace(ts.VideoDecision))
			if ctn == "srt" || strings.Contains(ctn, "srt") {
				subtitle = "copy"
			} else if vDecision == "transcode" {
				subtitle = "burn"
			}
		}

		_ = level.Info(l.log).Log("msg", "transcode session update", "sessionKey", ts.Key, "type", kind, "subtitle", subtitle)

		if l.activeSessions == nil {
			continue
		}

		matched := l.activeSessions.TrySetTranscodeType(ts.Key, kind)
		subMatched := l.activeSessions.TrySetSubtitleAction(ts.Key, subtitle)
		if matched && subMatched {
			continue
		}

		// refresh sessions and retry
		if l.conn != nil {
			if sessions, err := l.conn.GetSessions(); err == nil {
				for _, sess := range sessions.MediaContainer.Metadata {
					l.activeSessions.Update(sess.SessionKey, statePlaying, &sess, nil)
				}
				matched = l.activeSessions.TrySetTranscodeType(ts.Key, kind)
				subMatched = l.activeSessions.TrySetSubtitleAction(ts.Key, subtitle)
				if matched && subMatched {
					continue
				}
			}
		}

		// diagnostics and transcode sessions API fallback
		if l.conn != nil {
			// Build known sessions list from our in-memory store so logs include
			// both the map key and the inner SessionKey used in the metadata.
			if l.activeSessions != nil {
				// Build a concise known-sessions summary for diagnostics. Note that
				// tests and README examples intentionally populate session keys,
				// names, and user titles with randomized/sanitized values
				// (they mimic real-world shapes like numeric session keys or
				// UUID-like transcode IDs) so logs and test output don't leak
				// identifying information while keeping matching behavior
				// realistic.
				var ssum []string
				l.activeSessions.mtx.Lock()
				// Determine a stable ordering of sessions for deterministic logs
				var keys []string
				for k := range l.activeSessions.sessions {
					keys = append(keys, k)
				}
				l.activeSessions.mtx.Unlock()

				// sort keys outside the activeSessions lock to minimize lock hold time
				if len(keys) > 1 {
					// import sort at top of file
					// ...existing code...
					// sort keys to ensure deterministic output
					sort.Strings(keys)
				}

				l.activeSessions.mtx.Lock()
				for _, k := range keys {
					ss := l.activeSessions.sessions[k]
					// Skip orphaned sessions (those with no session key and no meaningful data) from diagnostic output
					if ss.session.SessionKey == "" && len(ss.session.Media) == 0 {
						continue
					}
					user := ss.session.User.Title
					player := ss.session.Player.Product
					inner := ss.session.SessionKey
					ssum = append(ssum, fmt.Sprintf("mapKey=%s sessionKey=%s user=%s player=%s", k, inner, user, player))
				}
				l.activeSessions.mtx.Unlock()

				// Only log warning if there are actual active sessions to show
				if len(ssum) > 0 {
					_ = level.Warn(l.log).Log("msg", "transcode update did not match any active session", "tsKey", ts.Key, "detectedKind", kind, "knownSessions", strings.Join(ssum, "; "))
				} else {
					_ = level.Debug(l.log).Log("msg", "transcode update for session not currently active", "tsKey", ts.Key, "detectedKind", kind)
				}
			} else {
				_ = level.Warn(l.log).Log("msg", "transcode update did not match and activeSessions is nil", "tsKey", ts.Key, "detectedKind", kind)
			}

			if tcs, err := l.conn.GetTranscodeSessions(); err == nil {
				var tsum []string
				for _, t := range tcs.Children {
					tsum = append(tsum, fmt.Sprintf("k=%s video=%s audio=%s decision=%s", t.Key, t.VideoCodec, t.AudioCodec, t.VideoDecision))
				}
				_ = level.Warn(l.log).Log("msg", "active transcode sessions", "list", strings.Join(tsum, "; "))

				for _, t := range tcs.Children {
					match := false
					if t.Key == ts.Key || strings.Contains(ts.Key, t.Key) || strings.Contains(t.Key, ts.Key) {
						match = true
					}
					if !match {
						continue
					}

					subFromAPI := ""
					if strings.TrimSpace(t.SubtitleDecision) != "" {
						sdec := strings.ToLower(strings.TrimSpace(t.SubtitleDecision))
						switch sdec {
						case "burn", "burn-in":
							subFromAPI = "burn"
						case "copy", "copying":
							subFromAPI = "copy"
						case "transcode", "transcoding":
							// Preserve API-reported "transcode" as its own
							// subtitle action so callers can distinguish it from
							// an explicit burn.
							subFromAPI = "transcode"
						}
					} else {
						ctn := strings.ToLower(strings.TrimSpace(t.Container))
						if ctn == "srt" || strings.Contains(ctn, "srt") {
							subFromAPI = "copy"
						} else if strings.ToLower(strings.TrimSpace(t.VideoDecision)) == "transcode" {
							subFromAPI = "burn"
						}
					}

					apiKind := ""
					vDecisionAPI := strings.ToLower(strings.TrimSpace(t.VideoDecision))
					aDecisionAPI := strings.ToLower(strings.TrimSpace(t.AudioDecision))
					if vDecisionAPI == "transcode" {
						if aDecisionAPI == "transcode" {
							apiKind = "both"
						} else {
							apiKind = "video"
						}
					} else if aDecisionAPI == "transcode" {
						apiKind = "audio"
					}

					if subFromAPI != "" {
						_ = level.Info(l.log).Log("msg", "derived subtitle action from transcode sessions API", "tsKey", t.Key, "subtitle", subFromAPI)
					}

					// apply to sessions (prefer TrySet, but create if necessary)
					applied := false
					if subFromAPI != "" {
						if l.activeSessions.TrySetSubtitleAction(ts.Key, subFromAPI) {
							subMatched = true
							applied = true
						} else {
							l.activeSessions.SetSubtitleAction(ts.Key, subFromAPI)
							subMatched = true
							applied = true
						}
					}
					if apiKind != "" {
						if l.activeSessions.TrySetTranscodeType(ts.Key, apiKind) {
							matched = true
							applied = true
						} else {
							l.activeSessions.SetTranscodeType(ts.Key, apiKind)
							matched = true
							applied = true
						}
					}
					if applied {
						break
					}
				}
			}
		}

		if !matched && !subMatched {
			l.activeSessions.SetTranscodeType(ts.Key, kind)
			l.activeSessions.SetSubtitleAction(ts.Key, subtitle)
		}
	}
}

func (l *plexListener) onPlaying(c plex.NotificationContainer) error {
	sessions, err := l.conn.GetSessions()
	if err != nil {
		return fmt.Errorf("error fetching sessions: %w", err)
	}

	for i, n := range c.PlaySessionStateNotification {
		if sessionState(n.State) == stateStopped {
			// When the session is stopped we can't look up the user info or media anymore.
			l.activeSessions.Update(n.SessionKey, sessionState(n.State), nil, nil)
			continue
		}

		// Try to resolve the session with exponential backoff. Notifications
		// can arrive slightly before the session list is updated; retry a few
		// times before giving up.
		session := getSessionByID(sessions, n.SessionKey)
		if session == nil {
			for attempt := 1; attempt <= SessionLookupMaxRetries && session == nil; attempt++ {
				// shift a duration to compute exponential backoff without int->uint conversion
				backoff := SessionLookupBaseDelay * (time.Duration(1) << (attempt - 1))
				time.Sleep(backoff)
				if s2, err := l.conn.GetSessions(); err == nil {
					session = getSessionByID(s2, n.SessionKey)
				} else {
					_ = level.Debug(l.log).Log("msg", "retrying GetSessions failed", "attempt", attempt, "err", err)
				}
			}
		}

		if session == nil {
			_ = level.Warn(l.log).Log("msg", "session not found for notification after retries, skipping", "SessionKey", n.SessionKey, "RatingKey", n.RatingKey, "state", n.State)
			continue
		}

		// Fetch metadata with retries in case the server hasn't populated it yet.
		var metadata plex.MediaMetadata
		var metaErr error
			for attempt := 1; attempt <= MetadataMaxRetries; attempt++ {
				metadata, metaErr = l.conn.GetMetadata(n.RatingKey)
				if metaErr == nil && len(metadata.MediaContainer.Metadata) > 0 {
					break
				}
				// if this was the last attempt break and we'll log below
				if attempt < MetadataMaxRetries {
					backoff := MetadataBaseDelay * (time.Duration(1) << (attempt - 1))
					time.Sleep(backoff)
				}
			}

		if metaErr != nil {
			_ = level.Error(l.log).Log("msg", "error fetching metadata for notification after retries, skipping", "RatingKey", n.RatingKey, "err", metaErr)
			continue
		}

		if len(metadata.MediaContainer.Metadata) == 0 {
			_ = level.Warn(l.log).Log("msg", "metadata response empty after retries, skipping", "RatingKey", n.RatingKey)
			continue
		}

		// Log only the first notification in the batch to keep logs concise
		// and structured. Always update sessions for every notification.
		if i == 0 {
			batchCount := len(c.PlaySessionStateNotification)
			_ = level.Info(l.log).Log("msg", "Received PlaySessionStateNotification",
				"SessionKey", n.SessionKey,
				"userName", session.User.Title,
				"userID", session.User.ID,
				"state", n.State,
				"mediaTitle", metadata.MediaContainer.Metadata[0].Title,
				"mediaID", metadata.MediaContainer.Metadata[0].RatingKey,
				"timestamp", time.Duration(time.Millisecond)*time.Duration(n.ViewOffset),
				"batchCount", batchCount,
			)
		}

		// Update the session-transcode mapping from the notification
		if l.activeSessions != nil {
			l.activeSessions.SetSessionTranscodeMapping(n.SessionKey, n.TranscodeSession)
		}

		l.activeSessions.Update(n.SessionKey, sessionState(n.State), session, &metadata.MediaContainer.Metadata[0])
	}

	return nil
}
