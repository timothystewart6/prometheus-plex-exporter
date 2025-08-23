// test_enhancements_test.go - Test examples for the new API enhancements
package plex

import (
	"encoding/json"
	"testing"
)

func TestFlexibleIntUnmarshaling(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		expected int64
	}{
		{
			name:     "String number",
			jsonData: `{"librarySectionID": "123"}`,
			expected: 123,
		},
		{
			name:     "Integer number",
			jsonData: `{"librarySectionID": 456}`,
			expected: 456,
		},
		{
			name:     "Empty string",
			jsonData: `{"librarySectionID": ""}`,
			expected: 0,
		},
		{
			name:     "Invalid string",
			jsonData: `{"librarySectionID": "abc"}`,
			expected: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var metadata Metadata
			if err := json.Unmarshal([]byte(test.jsonData), &metadata); err != nil {
				t.Errorf("Failed to unmarshal JSON: %v", err)
				return
			}

			actual := metadata.LibrarySectionID.Int64()

			if actual != test.expected {
				t.Errorf("Expected %d, got %d", test.expected, actual)
			}
		})
	}
}

func TestSubtitleDecisionInTranscodeSession(t *testing.T) {
	jsonData := `{
		"audioChannels": 2,
		"audioCodec": "aac",
		"audioDecision": "transcode",
		"complete": false,
		"container": "mkv",
		"context": "streaming",
		"duration": 7200000,
		"key": "transcode/session/abc123",
		"progress": 25.5,
		"protocol": "http",
		"remaining": 5400000,
		"sourceAudioCodec": "ac3",
		"sourceVideoCodec": "h264",
		"speed": 1.0,
		"subtitleDecision": "burn",
		"throttled": false,
		"transcodeHwRequested": true,
		"videoCodec": "h264",
		"videoDecision": "copy"
	}`

	var session TranscodeSession
	if err := json.Unmarshal([]byte(jsonData), &session); err != nil {
		t.Errorf("Failed to unmarshal TranscodeSession: %v", err)
		return
	}

	if session.SubtitleDecision != "burn" {
		t.Errorf("Expected SubtitleDecision to be 'burn', got '%s'", session.SubtitleDecision)
	}

	if session.AudioDecision != "transcode" {
		t.Errorf("Expected AudioDecision to be 'transcode', got '%s'", session.AudioDecision)
	}

	if session.VideoDecision != "copy" {
		t.Errorf("Expected VideoDecision to be 'copy', got '%s'", session.VideoDecision)
	}
}

func TestTimelineEventHandler(t *testing.T) {
	events := NewNotificationEvents()

	// Test that we can set a timeline handler
	events.OnTimeline(func(n NotificationContainer) {
		// Handler logic would go here
	})

	// Verify the handler was set by checking if the events map contains it
	if events.events["timeline"] == nil {
		t.Error("Timeline event handler was not set")
	}
}
