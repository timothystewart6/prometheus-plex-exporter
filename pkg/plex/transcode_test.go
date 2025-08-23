package plex

import (
	"testing"

	jrplex "github.com/jrudio/go-plex-client"
)

func TestTranscodeKind(t *testing.T) {
	tests := []struct {
		name string
		ts   jrplex.TranscodeSession
		want string
	}{
		{
			name: "video transcode",
			ts: jrplex.TranscodeSession{
				SourceVideoCodec: "h264",
				VideoCodec:       "hevc",
			},
			want: "video",
		},
		{
			name: "audio transcode",
			ts: jrplex.TranscodeSession{
				SourceAudioCodec: "aac",
				AudioCodec:       "mp3",
			},
			want: "audio",
		},
		{
			name: "both transcode",
			ts: jrplex.TranscodeSession{
				SourceVideoCodec: "h264",
				VideoCodec:       "vp9",
				SourceAudioCodec: "aac",
				AudioCodec:       "ac3",
			},
			want: "both",
		},
		{
			name: "unknown when empty",
			ts:   jrplex.TranscodeSession{},
			want: "unknown",
		},
		{
			name: "case and whitespace insensitive",
			ts: jrplex.TranscodeSession{
				SourceVideoCodec: " H264 ",
				VideoCodec:       "h264",
			},
			want: "unknown", // no change (source == target ignoring case/space)
		},
		{
			name: "heuristic video if only source present",
			ts: jrplex.TranscodeSession{
				SourceVideoCodec: "h264",
			},
			want: "video",
		},
		{
			name: "heuristic audio if only audio present",
			ts: jrplex.TranscodeSession{
				SourceAudioCodec: "aac",
			},
			want: "audio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := transcodeKind(tt.ts)
			if got != tt.want {
				t.Fatalf("transcodeKind() = %q, want %q", got, tt.want)
			}
		})
	}
}
