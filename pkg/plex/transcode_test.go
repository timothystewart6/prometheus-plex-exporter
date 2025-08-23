package plex

import (
	"testing"

	ttPlex "github.com/timothystewart6/go-plex-client"
)

func TestTranscodeKind(t *testing.T) {
	tests := []struct {
		name string
		ts   ttPlex.TranscodeSession
		want string
	}{
		{
			name: "video transcode",
			ts: ttPlex.TranscodeSession{
				SourceVideoCodec: "h264",
				VideoCodec:       "hevc",
			},
			want: "video",
		},
		{
			name: "audio transcode",
			ts: ttPlex.TranscodeSession{
				SourceAudioCodec: "aac",
				AudioCodec:       "mp3",
			},
			want: "audio",
		},
		{
			name: "both transcode",
			ts: ttPlex.TranscodeSession{
				SourceVideoCodec: "h264",
				VideoCodec:       "vp9",
				SourceAudioCodec: "aac",
				AudioCodec:       "ac3",
			},
			want: "both",
		},
		{
			name: "unknown when empty",
			ts:   ttPlex.TranscodeSession{},
			want: "unknown",
		},
		{
			name: "case and whitespace insensitive",
			ts: ttPlex.TranscodeSession{
				SourceVideoCodec: " H264 ",
				VideoCodec:       "h264",
			},
			want: "unknown", // no change (source == target ignoring case/space)
		},
		{
			name: "heuristic video if only source present",
			ts: ttPlex.TranscodeSession{
				SourceVideoCodec: "h264",
			},
			want: "video",
		},
		{
			name: "heuristic audio if only audio present",
			ts: ttPlex.TranscodeSession{
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
