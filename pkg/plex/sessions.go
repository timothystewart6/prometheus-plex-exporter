package plex

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/timothystewart6/go-plex-client"

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
	// persisted subtitle action for this session (burn|copy|none)
	subtitleAction string
}

type sessions struct {
	mtx      sync.Mutex
	sessions map[string]session
	// sessionTranscodeMap maps sessionKey to current transcodeSession ID
	// This is populated from PlaySessionStateNotification.transcodeSession
	sessionTranscodeMap            map[string]string
	server                         *Server
	totalEstimatedTransmittedKBits float64
}

func NewSessions(ctx context.Context, server *Server) *sessions {
	s := &sessions{
		sessions:            map[string]session{},
		sessionTranscodeMap: map[string]string{},
		server:              server,
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
		// Remove stopped sessions that are older than the timeout
		if v.state == stateStopped && time.Since(v.lastUpdate) > sessionTimeout {
			delete(s.sessions, k)
			delete(s.sessionTranscodeMap, k)
			continue
		}

		// Remove orphaned transcode sessions (sessions with no session key and no meaningful data) more aggressively
		// These are created when transcode notifications arrive for sessions that don't exist
		// Remove them if they're older than 10 seconds to allow for brief race conditions
		if v.session.SessionKey == "" && len(v.session.Media) == 0 &&
			(v.lastUpdate.IsZero() || time.Since(v.lastUpdate) > 10*time.Second) {
			delete(s.sessions, k)
			continue
		}
	}
}

// SetSessionTranscodeMapping establishes the mapping between a sessionKey and its transcodeSession ID.
// This is called when PlaySessionStateNotification events are received.
func (s *sessions) SetSessionTranscodeMapping(sessionKey, transcodeSessionID string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if transcodeSessionID != "" {
		s.sessionTranscodeMap[sessionKey] = transcodeSessionID
	} else {
		// If transcodeSessionID is empty, remove the mapping (direct play)
		delete(s.sessionTranscodeMap, sessionKey)
	}
}

// SetTranscodeType sets the transcode type for a given session id.
// It's safe to call concurrently with other session updates.
func (s *sessions) SetTranscodeType(sessionID, ttype string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// Fast-path: exact map key match
	if ss, ok := s.sessions[sessionID]; ok {
		ss.transcodeType = ttype
		s.sessions[sessionID] = ss
		return
	}

	// Try to find a session whose inner Metadata.SessionKey equals the
	// provided sessionID (some websocket notifications use slightly
	// different keys). If found, set the transcode type on that session.
	for k, ss := range s.sessions {
		if ss.session.SessionKey == sessionID {
			ss.transcodeType = ttype
			s.sessions[k] = ss
			return
		}
	}

	// Heuristic: attempt substring matches in either direction. This
	// handles cases where the notification key contains extra prefixes
	// or suffixes compared to the session key.
	for k, ss := range s.sessions {
		if ss.session.SessionKey != "" && (strings.Contains(sessionID, ss.session.SessionKey) || strings.Contains(ss.session.SessionKey, sessionID)) {
			ss.transcodeType = ttype
			s.sessions[k] = ss
			return
		}
	}

	// Fallback: create/set an entry under the provided key so tests that
	// expect this behavior continue to pass.
	ss := s.sessions[sessionID]
	ss.transcodeType = ttype
	s.sessions[sessionID] = ss
}

// SetSubtitleAction sets the subtitle action for a given session id.
func (s *sessions) SetSubtitleAction(sessionID, action string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if ss, ok := s.sessions[sessionID]; ok {
		ss.subtitleAction = action
		s.sessions[sessionID] = ss
		return
	}

	for k, ss := range s.sessions {
		if ss.session.SessionKey == sessionID {
			ss.subtitleAction = action
			s.sessions[k] = ss
			return
		}
	}

	for k, ss := range s.sessions {
		if ss.session.SessionKey != "" && (strings.Contains(sessionID, ss.session.SessionKey) || strings.Contains(ss.session.SessionKey, sessionID)) {
			ss.subtitleAction = action
			s.sessions[k] = ss
			return
		}
	}

	ss := s.sessions[sessionID]
	ss.subtitleAction = action
	s.sessions[sessionID] = ss
}

// TrySetSubtitleAction behaves like SetSubtitleAction but returns true if a
// session was found and updated.
func (s *sessions) TrySetSubtitleAction(sessionID, action string) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// If sessionID is a full transcode session path, extract just the session ID part
	originalSessionID := sessionID
	if strings.HasPrefix(sessionID, "/transcode/sessions/") {
		sessionID = extractTranscodeSessionID(sessionID)
	}

	if ss, ok := s.sessions[sessionID]; ok {
		ss.subtitleAction = action
		s.sessions[sessionID] = ss
		return true
	}

	for k, ss := range s.sessions {
		if ss.session.SessionKey == sessionID {
			ss.subtitleAction = action
			s.sessions[k] = ss
			return true
		}
	}

	// Check if any session has this transcode session ID mapped via PlaySessionStateNotification
	for sessionKey, mappedTranscodeID := range s.sessionTranscodeMap {
		extractedID := extractTranscodeSessionID(originalSessionID)
		if mappedTranscodeID == sessionID || mappedTranscodeID == originalSessionID || mappedTranscodeID == extractedID {
			if ss, ok := s.sessions[sessionKey]; ok {
				ss.subtitleAction = action
				s.sessions[sessionKey] = ss
				return true
			}
		}
	}

	for k, ss := range s.sessions {
		if ss.session.SessionKey != "" && (strings.Contains(sessionID, ss.session.SessionKey) || strings.Contains(ss.session.SessionKey, sessionID)) {
			ss.subtitleAction = action
			s.sessions[k] = ss
			return true
		}
	}

	// fallback: no match
	return false
}

// TrySetTranscodeType behaves like SetTranscodeType but returns true if a
// session was found and updated. This allows callers to detect no-match and
// emit additional debug information without duplicating matching logic.
func (s *sessions) TrySetTranscodeType(sessionID, ttype string) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// If sessionID is a full transcode session path, extract just the session ID part
	originalSessionID := sessionID
	if strings.HasPrefix(sessionID, "/transcode/sessions/") {
		sessionID = extractTranscodeSessionID(sessionID)
	}

	// Helper: return last path segment for loose/suffix matching
	lastSeg := func(p string) string {
		if p == "" {
			return ""
		}
		if strings.Contains(p, "/") {
			parts := strings.Split(p, "/")
			return parts[len(parts)-1]
		}
		return p
	}

	if ss, ok := s.sessions[sessionID]; ok {
		ss.transcodeType = ttype
		s.sessions[sessionID] = ss
		return true
	}

	// Exact match against stored map key
	for k, ss := range s.sessions {
		if k == sessionID {
			ss.transcodeType = ttype
			s.sessions[k] = ss
			return true
		}
	}

	// Exact match against inner SessionKey
	for k, ss := range s.sessions {
		if ss.session.SessionKey == sessionID {
			ss.transcodeType = ttype
			s.sessions[k] = ss
			return true
		}
	}

	// Suffix/last-segment tolerant matching: compare last segments both ways
	for k, ss := range s.sessions {
		if ss.session.SessionKey != "" {
			if lastSeg(sessionID) == lastSeg(ss.session.SessionKey) {
				ss.transcodeType = ttype
				s.sessions[k] = ss
				return true
			}
			if strings.Contains(sessionID, ss.session.SessionKey) || strings.Contains(ss.session.SessionKey, sessionID) {
				ss.transcodeType = ttype
				s.sessions[k] = ss
				return true
			}
		}
	}

	// Check if any session has this transcode session ID mapped via PlaySessionStateNotification
	for sessionKey, mappedTranscodeID := range s.sessionTranscodeMap {
		extractedID := extractTranscodeSessionID(originalSessionID)
		if mappedTranscodeID == sessionID || mappedTranscodeID == originalSessionID || mappedTranscodeID == extractedID {
			if ss, ok := s.sessions[sessionKey]; ok {
				ss.transcodeType = ttype
				s.sessions[sessionKey] = ss
				return true
			}
		}
	}

	// Check if the transcode session matches any of the transcode sessions embedded in active session media
	for k, ss := range s.sessions {
		// Skip sessions without proper session data (orphaned transcode entries)
		if ss.session.SessionKey == "" || ss.session.User.Title == "" {
			continue
		}

		if len(ss.session.Media) > 0 {
			for _, media := range ss.session.Media {
				for _, part := range media.Part {
					if part.Key != "" && strings.Contains(part.Key, "/transcode/sessions/") {
						partTranscodeID := extractTranscodeSessionID(part.Key)
						if partTranscodeID == sessionID || partTranscodeID == extractTranscodeSessionID(originalSessionID) {
							ss.transcodeType = ttype
							s.sessions[k] = ss
							return true
						}
					}
				}
			}
		}
	}

	// Enhanced transcode session matching: check if the websocket sessionID
	// matches any transcode session keys embedded in the session data.
	// This handles cases where websocket sends keys like "abc123" or "/transcode/sessions/abc123" but
	// session data contains transcode session paths like "/transcode/sessions/abc123".
	// Prefer play-session transcode keys found in media parts: treat these as authoritative
	for k, ss := range s.sessions {
		if len(ss.session.Media) == 0 {
			continue
		}
		for _, media := range ss.session.Media {
			for _, part := range media.Part {
				if part.Key == "" {
					continue
				}

				// If the media part contains a transcode sessions path, prefer that mapping
				if strings.Contains(part.Key, "/transcode/sessions/") {
					partID := extractTranscodeSessionID(part.Key)
					if partID != "" {
						if partID == sessionID || partID == originalSessionID || lastSeg(partID) == lastSeg(sessionID) {
							ss.transcodeType = ttype
							s.sessions[k] = ss
							return true
						}
					}
				}

				// Direct match against original full path
				if originalSessionID != sessionID && part.Key == originalSessionID {
					ss.transcodeType = ttype
					s.sessions[k] = ss
					return true
				}
			}
		}
	}

	// No existing session matched.
	// Heuristic fallback: apply to any session that currently has a
	// part decision of "transcode". This handles cases where the
	// websocket transcode key doesn't map directly to our session map
	// keys but the server is clearly performing a transcode for a
	// session we already track.
	//
	// Enhanced heuristic: try multiple strategies in order of preference

	// Strategy 1: Apply to any single transcoding session if there's only one
	transcodingSessions := []string{}
	for k, ss := range s.sessions {
		// Skip orphaned transcode sessions (those with no session key and no meaningful data)
		if ss.session.SessionKey == "" && len(ss.session.Media) == 0 {
			continue
		}

		if len(ss.session.Media) > 0 && len(ss.session.Media[0].Part) > 0 {
			if ss.session.Media[0].Part[0].Decision == "transcode" {
				transcodingSessions = append(transcodingSessions, k)
			}
		}
	}

	// If there's exactly one transcoding session, apply the update to it
	if len(transcodingSessions) == 1 {
		k := transcodingSessions[0]
		ss := s.sessions[k]
		ss.transcodeType = ttype
		s.sessions[k] = ss
		return true
	}

	// Strategy 2: Apply to most recent session if we have multiple transcoding sessions
	if len(transcodingSessions) > 1 {
		var mostRecentKey string
		var mostRecentTime time.Time

		for _, k := range transcodingSessions {
			ss := s.sessions[k]
			if ss.playStarted.After(mostRecentTime) {
				mostRecentTime = ss.playStarted
				mostRecentKey = k
			}
		}

		if mostRecentKey != "" {
			ss := s.sessions[mostRecentKey]
			ss.transcodeType = ttype
			s.sessions[mostRecentKey] = ss
			return true
		}
	}

	return false
}

// extractTranscodeSessionID extracts the session ID from a transcode session path.
// For example, "/transcode/sessions/abc123" returns "abc123".
func extractTranscodeSessionID(path string) string {
	const transcodePrefix = "/transcode/sessions/"
	if strings.Contains(path, transcodePrefix) {
		// Find the position after the prefix
		if idx := strings.Index(path, transcodePrefix); idx >= 0 {
			sessionPart := path[idx+len(transcodePrefix):]
			// Return everything up to the next slash or end of string
			if slashIdx := strings.Index(sessionPart, "/"); slashIdx >= 0 {
				return sessionPart[:slashIdx]
			}
			return sessionPart
		}
	}
	return ""
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
			// Be defensive: session.Media may be empty in some edge cases
			// (for example, when a websocket event arrived before metadata was
			// fully populated). Only use the bitrate when available.
			if len(ss.session.Media) > 0 {
				total += time.Since(ss.playStarted).Seconds() * float64(ss.session.Media[0].Bitrate)
			}
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

		// Defensive extraction of nested fields that may be missing when
		// websocket notifications arrive before full metadata is available.
		streamType := "unknown"
		sessionRes := ""
		fileRes := ""
		bitrate := "0"
		device := ""
		product := ""
		user := ""

		if len(session.session.Media) > 0 {
			if len(session.session.Media[0].Part) > 0 {
				if session.session.Media[0].Part[0].Decision != "" {
					streamType = session.session.Media[0].Part[0].Decision
				}
			}
			if session.session.Media[0].VideoResolution != "" {
				sessionRes = session.session.Media[0].VideoResolution
			}
			if session.session.Media[0].Bitrate != 0 {
				bitrate = strconv.Itoa(session.session.Media[0].Bitrate)
			}
			device = session.session.Player.Device
			product = session.session.Player.Product
			user = session.session.User.Title
		}
		if len(session.media.Media) > 0 {
			if session.media.Media[0].VideoResolution != "" {
				fileRes = session.media.Media[0].VideoResolution
			}
		}

		ch <- metrics.Play(
			1.0,
			"plex",
			s.server.Name,
			s.server.ID,
			libraryType,
			libraryName,
			libraryID,
			session.media.Type,
			title,
			season,
			episode,
			streamType, // stream type
			sessionRes, // stream res
			fileRes,    // file res
			bitrate,    // bitrate
			device,     // device
			product,    // device type
			user,
			id,
			session.transcodeType,
			session.subtitleAction,
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
			libraryType,
			libraryName,
			libraryID,
			session.media.Type,
			title,
			season,
			episode,
			streamType, // stream type
			sessionRes, // stream res
			fileRes,    // file res
			bitrate,    // bitrate
			device,     // device
			product,    // device type
			user,
			id,
			session.transcodeType,
			session.subtitleAction,
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
