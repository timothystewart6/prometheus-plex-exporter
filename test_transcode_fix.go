package main

import (
	"fmt"
	"strings"
)

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

func main() {
	// Test cases for the extractTranscodeSessionID function
	testCases := []struct {
		input    string
		expected string
	}{
		{"/transcode/sessions/zplafkzqz7buwjittgvojjyl", "zplafkzqz7buwjittgvojjyl"},
		{"/transcode/sessions/abc123/segments/0", "abc123"},
		{"/transcode/sessions/test-session", "test-session"},
		{"/some/other/path", ""},
		{"", ""},
		{"/transcode/sessions/", ""},
		{"no-transcode-in-path", ""},
	}

	fmt.Println("Testing extractTranscodeSessionID function:")
	for i, tc := range testCases {
		result := extractTranscodeSessionID(tc.input)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("Test %d: %s\n", i+1, status)
		fmt.Printf("  Input:    %q\n", tc.input)
		fmt.Printf("  Expected: %q\n", tc.expected)
		fmt.Printf("  Got:      %q\n", result)
		fmt.Println()
	}
}
