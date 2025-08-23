package plex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/websocket"
	"github.com/jrudio/go-plex-client"
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
var newPlex = plex.New

type plexClient interface {
	GetSessions() (plex.CurrentSessions, error)
	GetMetadata(string) (plex.MediaMetadata, error)
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

	// forward context completion to jrudio/go-plex-client
	ctrlC := make(chan os.Signal, 1)
	go func() {
		<-ctx.Done()
		close(ctrlC)
	}()

	doneChan := make(chan error, 1)
	onError := func(err error) {
		defer close(doneChan)
		var closeErr *websocket.CloseError
		if errors.As(err, &closeErr) {
			if closeErr.Code == websocket.CloseNormalClosure {
				return
			}
		}
		_ = level.Error(log).Log("msg", "error in websocket processing", "err", err)
		doneChan <- err
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
func (l *plexListener) onTranscodeUpdateHandler(c plex.NotificationContainer) {
	if len(c.TranscodeSession) == 0 {
		return
	}

	// process each transcode session
	for _, ts := range c.TranscodeSession {
		kind := transcodeKind(ts)
		// determine subtitle action: prefer explicit subtitleDecision when present
		subtitle := "none"
		sd := strings.ToLower(strings.TrimSpace(ts.SubtitleDecision))
		if sd != "" {
			if sd == "burn" || sd == "burn-in" {
				subtitle = "burn"
			} else if sd == "copy" || sd == "copying" {
				subtitle = "copy"
			}
		} else {
			// fallback heuristics: prefer explicit container indicating sidecar
			c := strings.ToLower(strings.TrimSpace(ts.Container))
			vDecision := strings.ToLower(strings.TrimSpace(ts.VideoDecision))
			if c == "srt" || strings.Contains(c, "srt") {
				subtitle = "copy"
			} else if vDecision == "transcode" {
				// subtitle burn-in usually causes video to be transcoded
				subtitle = "burn"
			}
		}

		_ = level.Info(l.log).Log("msg", "transcode session update", "sessionKey", ts.Key, "type", kind, "subtitle", subtitle)
		// persist transcode type and subtitle action for the session so Collect() can emit the label
		if l.activeSessions != nil {
			// Try a best-effort match and if it fails, fetch current sessions
			// and active transcode sessions to aid debugging.
			matched := l.activeSessions.TrySetTranscodeType(ts.Key, kind)
			subMatched := l.activeSessions.TrySetSubtitleAction(ts.Key, subtitle)
			if matched && subMatched {
				continue
			}

			// If we failed to match the transcode update to an active session,
			// try refreshing the server session list and populate our sessions
			// map; websocket notifications can arrive slightly before the
			// initial session list is available.
			if conn, ok := l.conn.(*plex.Plex); ok {
				if sessions, err := conn.GetSessions(); err == nil {
					for _, sess := range sessions.MediaContainer.Metadata {
						// Use statePlaying as these are active sessions reported by the server
						// We don't have the full Media metadata here; pass nil for media.
						l.activeSessions.Update(sess.SessionKey, statePlaying, &sess, nil)
					}
					// Retry matching after refresh
					matched = l.activeSessions.TrySetTranscodeType(ts.Key, kind)
					subMatched = l.activeSessions.TrySetSubtitleAction(ts.Key, subtitle)
					if matched && subMatched {
						continue
					}
				}
			}

			// No match: log concise diagnostics. Avoid expensive operations
			// in hot paths, but this helps debugging mismatched keys.
			if conn, ok := l.conn.(*plex.Plex); ok {
				if sessions, err := conn.GetSessions(); err == nil {
					// Summarize session keys and player info
					var ssum []string
					for _, sess := range sessions.MediaContainer.Metadata {
						ssum = append(ssum, fmt.Sprintf("k=%s user=%s player=%s", sess.SessionKey, sess.User.Title, sess.Player.Product))
					}
					_ = level.Warn(l.log).Log("msg", "transcode update did not match any active session", "tsKey", ts.Key, "detectedKind", kind, "knownSessions", strings.Join(ssum, "; "))
				} else {
					_ = level.Warn(l.log).Log("msg", "transcode update match failed and GetSessions failed", "tsKey", ts.Key, "err", err)
				}

				if tcs, err := conn.GetTranscodeSessions(); err == nil {
					var tsum []string
					for _, t := range tcs.Children {
						tsum = append(tsum, fmt.Sprintf("k=%s video=%s audio=%s decision=%s", t.Key, t.VideoCodec, t.AudioCodec, t.VideoDecision))
					}
					_ = level.Warn(l.log).Log("msg", "active transcode sessions", "list", strings.Join(tsum, "; "))

					// Attempt to find a matching transcode session entry and use it
					// as a source of truth for subtitleDecision (and re-try matching
					// the sessions map). This covers cases where websocket payloads
					// omit subtitleDecision but the HTTP endpoint exposes it.
					for _, t := range tcs.Children {
						match := false
						if t.Key == ts.Key || strings.Contains(ts.Key, t.Key) || strings.Contains(t.Key, ts.Key) {
							match = true
						}
						if !match {
							continue
						}
						// Derive subtitle from explicit field if present, otherwise fall back.
						subFromAPI := ""
						if strings.TrimSpace(t.SubtitleDecision) != "" {
							sd := strings.ToLower(strings.TrimSpace(t.SubtitleDecision))
							if sd == "burn" || sd == "burn-in" {
								subFromAPI = "burn"
							} else if sd == "copy" || sd == "copying" {
								subFromAPI = "copy"
							}
						} else {
							c := strings.ToLower(strings.TrimSpace(t.Container))
							if c == "srt" || strings.Contains(c, "srt") {
								subFromAPI = "copy"
							} else if strings.ToLower(strings.TrimSpace(t.VideoDecision)) == "transcode" {
								subFromAPI = "burn"
							}
						}

						if subFromAPI != "" || true {
							// derive an explicit transcode kind from the API when possible
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
							// Try to persist to the session entry
							if l.activeSessions != nil {
								applied := false
								if subFromAPI != "" {
									if l.activeSessions.TrySetSubtitleAction(ts.Key, subFromAPI) {
										subMatched = true
										applied = true
									}
									// try setting transcode type from API kind if present
									if apiKind != "" {
										if l.activeSessions.TrySetTranscodeType(ts.Key, apiKind) {
											matched = true
											applied = true
										}
									}
								}
								// if we didn't apply anything yet, still attempt to set transcode type
								if !applied && apiKind != "" {
									if l.activeSessions.TrySetTranscodeType(ts.Key, apiKind) {
										matched = true
									}
								}
								if applied {
									break
								}
							}
						}
					}
				}
			}
			// Ensure we still set the transcode type and subtitle action (fallback/create behavior).
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
				time.Sleep(SessionLookupBaseDelay * time.Duration(1<<uint(attempt-1)))
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
				time.Sleep(MetadataBaseDelay * time.Duration(1<<uint(attempt-1)))
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

		l.activeSessions.Update(n.SessionKey, sessionState(n.State), session, &metadata.MediaContainer.Metadata[0])
	}

	return nil
}
