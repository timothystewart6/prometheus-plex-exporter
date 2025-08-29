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

func TestTranscodeKind_RealWorldScenarios(t *testing.T) {
	// Test cases based on actual Prometheus data patterns
	testCases := []struct {
		name     string
		session  ttPlex.TranscodeSession
		expected string
	}{
		{
			name: "Fake_Movie_A_Both_Transcode",
			session: ttPlex.TranscodeSession{
				VideoDecision:    "transcode",
				AudioDecision:    "transcode",
				VideoCodec:       "h264",
				AudioCodec:       "aac",
				SourceVideoCodec: "h264",
				SourceAudioCodec: "ac3",
			},
			expected: "both",
		},
		{
			name: "Fake_Movie_B_DirectPlay",
			session: ttPlex.TranscodeSession{
				VideoDecision:    "directplay",
				AudioDecision:    "directplay",
				VideoCodec:       "h264",
				AudioCodec:       "aac",
				SourceVideoCodec: "h264",
				SourceAudioCodec: "aac",
			},
			expected: "unknown", // No actual transcoding happening
		},
		{
			name: "Fake_Device_TV_Both_Transcode",
			session: ttPlex.TranscodeSession{
				VideoDecision:    "transcode",
				AudioDecision:    "transcode",
				VideoCodec:       "h264",
				AudioCodec:       "aac",
				SourceVideoCodec: "hevc",
				SourceAudioCodec: "dts",
			},
			expected: "both",
		},
		{
			name: "Fake_Streaming_Device_Video_Only_Transcode",
			session: ttPlex.TranscodeSession{
				VideoDecision:    "transcode",
				AudioDecision:    "directplay",
				VideoCodec:       "h264",
				AudioCodec:       "aac",
				SourceVideoCodec: "hevc",
				SourceAudioCodec: "aac",
			},
			expected: "video",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := transcodeKind(tc.session)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s for %s", tc.expected, result, tc.name)
			}
		})
	}
}
