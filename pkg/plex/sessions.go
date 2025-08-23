package plex

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/jrudio/go-plex-client"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/plexporter/pkg/metrics"
)

type sessionState string

const (
	statePlaying   sessionState = "playing"
	stateStopped   sessionState = "stopped"
	statePaused    sessionState = "paused"
	stateBuffering sessionState = "buffering"

	mediaTypeEpisode = "episode"

	// How long metrics for sessions are kept after the last update.
	// This is used to prune prometheus metrics and keep cardinality
	// down.
	sessionTimeout = time.Minute
)

type session struct {
	session        plex.Metadata
	media          plex.Metadata
	state          sessionState
	lastUpdate     time.Time
	playStarted    time.Time
	prevPlayedTime time.Duration
	// Persisted library labels resolved when the media was first seen.
	// Keeping these prevents metric label churn if server library lookup
	// temporarily fails later.
	resolvedLibraryName string
	resolvedLibraryID   string
	resolvedLibraryType string
	// persisted transcode type for this session, set when transcode events
	// are received via the websocket. Helps keep metrics stable and avoid
	// trying to infer transcode type at collection time.
	transcodeType string
}

type sessions struct {
	mtx                            sync.Mutex
	sessions                       map[string]session
	server                         *Server
	totalEstimatedTransmittedKBits float64
}

func NewSessions(ctx context.Context, server *Server) *sessions {
	s := &sessions{
		sessions: map[string]session{},
		server:   server,
	}

	ticker := time.NewTicker(time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.pruneOldSessions()
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()

	return s
}

func (s *sessions) pruneOldSessions() {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for k, v := range s.sessions {
		if v.state == stateStopped && time.Since(v.lastUpdate) > sessionTimeout {
			delete(s.sessions, k)
		}
	}
}

// SetTranscodeType sets the transcode type for a given session id.
// It's safe to call concurrently with other session updates.
func (s *sessions) SetTranscodeType(sessionID, ttype string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	ss := s.sessions[sessionID]
	ss.transcodeType = ttype
	s.sessions[sessionID] = ss
}

func (s *sessions) Update(sessionID string, newState sessionState, newSession *plex.Metadata, media *plex.Metadata) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	ss := s.sessions[sessionID]

	if newSession != nil {
		ss.session = *newSession
	}

	if media != nil {
		ss.media = *media
		// Attempt to resolve the library for this media and persist the
		// labels so later Collect() can emit stable labels even if the
		// server's library list is temporarily unavailable.
		if ss.resolvedLibraryName == "" {
			if media.LibrarySectionID.Int64() != 0 {
				lib := s.server.Library(strconv.FormatInt(media.LibrarySectionID.Int64(), 10))
				if lib != nil {
					ss.resolvedLibraryName = lib.Name
					ss.resolvedLibraryID = lib.ID
					ss.resolvedLibraryType = lib.Type
				}
			}
		}
	}

	if ss.state == statePlaying && newState != statePlaying {
		// If the session was playing but now is not, then flatten
		// the play time into the total.
		ss.prevPlayedTime += time.Since(ss.playStarted)
		s.totalEstimatedTransmittedKBits += time.Since(ss.playStarted).Seconds() * float64(ss.session.Media[0].Bitrate)
	}

	if ss.state != statePlaying && newState == statePlaying {
		// Started playing
		ss.playStarted = time.Now()
	}

	ss.state = newState
	ss.lastUpdate = time.Now()
	s.sessions[sessionID] = ss
}

func (s *sessions) extrapolatedTransmittedBytes() float64 {

	total := s.totalEstimatedTransmittedKBits

	for _, ss := range s.sessions {
		if ss.state == statePlaying {
			total += time.Since(ss.playStarted).Seconds() * float64(ss.session.Media[0].Bitrate)
		}
	}

	return total * 128.0 // Kbits -> Bytes, 1024 / 8
}

func (s *sessions) Describe(ch chan<- *prometheus.Desc) {
	ch <- metrics.MetricPlayCountDesc
	ch <- metrics.MetricPlaySecondsTotalDesc

	ch <- metrics.MetricEstimatedTransmittedBytesTotal
}

func (s *sessions) Collect(ch chan<- prometheus.Metric) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for id, session := range s.sessions {
		if session.playStarted.IsZero() {
			continue
		}

		title, season, episode := labels(session.media)

		// Prefer persisted resolved labels if available; fall back to
		// live lookup and then to 'unknown' if necessary.
		var libraryName, libraryID, libraryType string
		if session.resolvedLibraryName != "" {
			libraryName = session.resolvedLibraryName
			libraryID = session.resolvedLibraryID
			libraryType = session.resolvedLibraryType
		} else {
			library := s.server.Library(strconv.FormatInt(session.media.LibrarySectionID.Int64(), 10))
			if library == nil {
				libraryName = "unknown"
				libraryID = "0"
				libraryType = "unknown"
			} else {
				libraryName = library.Name
				libraryID = library.ID
				libraryType = library.Type
			}
		}

		ch <- metrics.Play(
			1.0,
			"plex",
			s.server.Name,
			s.server.ID,
			libraryName,
			libraryID,
			libraryType,
			session.media.Type,
			title,
			season,
			episode,
			session.session.Media[0].Part[0].Decision,      // stream type
			session.session.Media[0].VideoResolution,       // stream res
			session.media.Media[0].VideoResolution,         // file res
			strconv.Itoa(session.session.Media[0].Bitrate), // bitrate
			session.session.Player.Device,                  // device
			session.session.Player.Product,                 // device type
			session.session.User.Title,
			id,
			session.transcodeType,
		)

		totalPlayTime := session.prevPlayedTime
		if session.state == statePlaying {
			totalPlayTime += time.Since(session.playStarted)
		}

		ch <- metrics.PlayDuration(
			float64(totalPlayTime.Seconds()),
			"plex",
			s.server.Name,
			s.server.ID,
			libraryName,
			libraryID,
			libraryType,
			session.media.Type,
			title,
			season,
			episode,
			session.session.Media[0].Part[0].Decision,      // stream type
			session.session.Media[0].VideoResolution,       // stream res
			session.media.Media[0].VideoResolution,         // file res
			strconv.Itoa(session.session.Media[0].Bitrate), // bitrate
			session.session.Player.Device,                  // device
			session.session.Player.Product,                 // device type
			session.session.User.Title,
			id,
			session.transcodeType,
		)
	}

	ch <- prometheus.MustNewConstMetric(metrics.MetricEstimatedTransmittedBytesTotal, prometheus.CounterValue, s.extrapolatedTransmittedBytes(), "plex", s.server.Name,
		s.server.ID)
}

func labels(m plex.Metadata) (title, season, episodeTitle string) {
	if m.Type == mediaTypeEpisode {
		return m.GrandparentTitle, m.ParentTitle, m.Title
	}
	return m.Title, "", ""
}
