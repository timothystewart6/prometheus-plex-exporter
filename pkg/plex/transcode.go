package plex

import (
	"strings"

	jrplex "github.com/jrudio/go-plex-client"
)

// transcodeKind returns one of: "video", "audio", "both", or "unknown".
// It inspects source vs. target codec fields in the TranscodeSession reported
// by the Plex websocket notification.
func transcodeKind(ts jrplex.TranscodeSession) string {
	vSrc := strings.ToLower(strings.TrimSpace(ts.SourceVideoCodec))
	vNew := strings.ToLower(strings.TrimSpace(ts.VideoCodec))
	aSrc := strings.ToLower(strings.TrimSpace(ts.SourceAudioCodec))
	aNew := strings.ToLower(strings.TrimSpace(ts.AudioCodec))

	hasVideoChange := vNew != "" && vNew != vSrc
	hasAudioChange := aNew != "" && aNew != aSrc

	if hasVideoChange {
		if hasAudioChange {
			return "both"
		}
		return "video"
	}
	if hasAudioChange {
		return "audio"
	}

	// No explicit codec change detected.
	// If target codec(s) are present and equal to source, treat as "unknown"
	if (vNew != "" && vNew == vSrc) || (aNew != "" && aNew == aSrc) {
		return "unknown"
	}

	// Heuristic: if only source codecs are present (no target), infer type.
	if vNew == "" && vSrc != "" && aNew == "" && aSrc != "" {
		return "both"
	}
	if vNew == "" && vSrc != "" {
		return "video"
	}
	if aNew == "" && aSrc != "" {
		return "audio"
	}

	return "unknown"
}
