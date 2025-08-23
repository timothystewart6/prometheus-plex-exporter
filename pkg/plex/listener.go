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
