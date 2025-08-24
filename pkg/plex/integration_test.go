package plex

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	ttPlex "github.com/timothystewart6/go-plex-client"
)

// TestIntegration_RealWorldMetrics creates integration tests based on actual Prometheus metrics
// captured from a live Plex server. This test validates that the exporter correctly handles
// real-world scenarios including:
// - Multiple concurrent users (User1, User2, User3, User4, User5)
// - Various devices (OSX/Plex Web, VizioTV, RokuTV001, Apple TV)
// - Different streaming scenarios (direct play vs transcode)
// - Popular movies (Wizard of Oz, Back to the Future, The Goonies, The Karate Kid, Batman Begins)
// - 4K video resolutions and mixed bitrates
//
// This ensures the exporter will work correctly with actual usage patterns.

// mockPlexData represents realistic data from  Prometheus metrics
type mockPlexData struct {
	server   mockServerInfo
	sessions []mockSession
}

type mockServerInfo struct {
	friendlyName      string
	machineIdentifier string
	version           string
}

type mockSession struct {
	sessionID string
	session   ttPlex.Metadata
	media     ttPlex.Metadata
}

// createMockPlexData generates test data based on real Prometheus metrics
func createMockPlexData() mockPlexData {
	return mockPlexData{
		server: mockServerInfo{
			friendlyName:      "Plex Server",
			machineIdentifier: "a7f3b2c8e9d1456789abcdef0123456789fedcba9",
			version:           "1.32.5.123",
		},
		sessions: []mockSession{
			{
				sessionID: "328",
				session: ttPlex.Metadata{
					SessionKey: "328",
					User: ttPlex.User{
						Title: "User1",
					},
					Player: ttPlex.Player{
						Platform: "OSX",
						Product:  "Plex Web",
						Title:    "OSX",
						Device:   "OSX",
					},
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "Wizard of Oz",
					ViewOffset:       370575, // milliseconds
					Media: []ttPlex.Media{
						{
							Bitrate:         20256,
							VideoResolution: "4k",
							Container:       "mkv",
							Part: []ttPlex.Part{
								{
									Decision: "transcode",
								},
							},
						},
					},
				},
				media: ttPlex.Metadata{
					RatingKey:        "test001",
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "Wizard of Oz",
					Media: []ttPlex.Media{
						{
							Bitrate:         20256,
							VideoResolution: "4k",
							Container:       "mkv",
						},
					},
				},
			},
			{
				sessionID: "329",
				session: ttPlex.Metadata{
					SessionKey: "329",
					User: ttPlex.User{
						Title: "User2",
					},
					Player: ttPlex.Player{
						Platform: "VizioTV",
						Product:  "Plex for Vizio",
						Title:    "Vizio SmartCast",
						Device:   "Vizio SmartCast",
					},
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "Back to the Future",
					ViewOffset:       9265062, // milliseconds
					Media: []ttPlex.Media{
						{
							Bitrate:         7351,
							VideoResolution: "4k",
							Container:       "mp4",
							Part: []ttPlex.Part{
								{
									Decision: "directplay",
								},
							},
						},
					},
				},
				media: ttPlex.Metadata{
					RatingKey:        "test002",
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "Back to the Future",
					Media: []ttPlex.Media{
						{
							Bitrate:         7351,
							VideoResolution: "4k",
							Container:       "mp4",
						},
					},
				},
			},
			{
				sessionID: "330",
				session: ttPlex.Metadata{
					SessionKey: "330",
					User: ttPlex.User{
						Title: "User3",
					},
					Player: ttPlex.Player{
						Platform: "OSX",
						Product:  "Plex Web",
						Title:    "OSX",
						Device:   "OSX",
					},
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "The Goonies",
					ViewOffset:       4576965, // milliseconds
					Media: []ttPlex.Media{
						{
							Bitrate:         20256,
							VideoResolution: "4k",
							Container:       "mkv",
							Part: []ttPlex.Part{
								{
									Decision: "transcode",
								},
							},
						},
					},
				},
				media: ttPlex.Metadata{
					RatingKey:        "test003",
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "The Goonies",
					Media: []ttPlex.Media{
						{
							Bitrate:         20256,
							VideoResolution: "4k",
							Container:       "mkv",
						},
					},
				},
			},
			{
				sessionID: "335",
				session: ttPlex.Metadata{
					SessionKey: "335",
					User: ttPlex.User{
						Title: "User4",
					},
					Player: ttPlex.Player{
						Platform: "RokuTV001",
						Product:  "Plex for Roku",
						Title:    "RokuTV001",
						Device:   "RokuTV001",
					},
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "The Karate Kid",
					ViewOffset:       2949055, // milliseconds
					Media: []ttPlex.Media{
						{
							Bitrate:         6251,
							VideoResolution: "4k",
							Container:       "mp4",
							Part: []ttPlex.Part{
								{
									Decision: "transcode",
								},
							},
						},
					},
				},
				media: ttPlex.Metadata{
					RatingKey:        "test004",
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "The Karate Kid",
					Media: []ttPlex.Media{
						{
							Bitrate:         6251,
							VideoResolution: "4k",
							Container:       "mp4",
						},
					},
				},
			},
			{
				sessionID: "336",
				session: ttPlex.Metadata{
					SessionKey: "336",
					User: ttPlex.User{
						Title: "User5",
					},
					Player: ttPlex.Player{
						Platform: "Apple TV",
						Product:  "Plex for Apple TV",
						Title:    "Apple TV",
						Device:   "Apple TV",
					},
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "Batman Begins",
					ViewOffset:       3227773, // milliseconds
					Media: []ttPlex.Media{
						{
							Bitrate:         11178,
							VideoResolution: "4k",
							Container:       "mkv",
							Part: []ttPlex.Part{
								{
									Decision: "transcode",
								},
							},
						},
					},
				},
				media: ttPlex.Metadata{
					RatingKey:        "test005",
					LibrarySectionID: ttPlex.FlexibleInt64(1),
					Type:             "movie",
					Title:            "Batman Begins",
					Media: []ttPlex.Media{
						{
							Bitrate:         11178,
							VideoResolution: "4k",
							Container:       "mkv",
						},
					},
				},
			},
		},
	}
}

func TestIntegration_RealWorldMetrics(t *testing.T) {
	// Create mock data based on real Prometheus metrics
	mockData := createMockPlexData()

	// Create mock server with libraries
	server := &Server{
		ID:      mockData.server.machineIdentifier,
		Name:    mockData.server.friendlyName,
		Version: mockData.server.version,
		libraries: []*Library{
			{
				ID:         "1",
				Name:       "Movies",
				Type:       "movie",
				ItemsCount: 150,
			},
		},
	}

	// Create sessions manager
	sess := NewSessions(context.Background(), server)

	// Simulate active sessions like in Prometheus data
	for _, mockSession := range mockData.sessions {
		sess.Update(
			mockSession.sessionID,
			statePlaying, // Use the correct state constant
			&mockSession.session,
			&mockSession.media,
		)

		// Set realistic transcode and subtitle info based on data
		switch mockSession.sessionID {
		case "328", "330", "335", "336": // These were transcoding in data
			sess.SetTranscodeType(mockSession.sessionID, "both")
		case "329": // Back to the Future was direct play
			sess.SetTranscodeType(mockSession.sessionID, "none")
		default:
			sess.SetTranscodeType(mockSession.sessionID, "unknown")
		}
	}

	// Test metrics collection
	t.Run("CollectPlayMetrics", func(t *testing.T) {
		// Create channels for describe and collect
		descCh := make(chan *prometheus.Desc, 100)
		metricCh := make(chan prometheus.Metric, 100)

		// Describe and collect metrics
		sess.Describe(descCh)
		close(descCh)

		sess.Collect(metricCh)
		close(metricCh)

		// Count metrics and debug output
		playMetrics := 0
		durationMetrics := 0
		totalMetrics := 0
		expectedUsers := map[string]bool{
			"User1": false, "User2": false, "User3": false,
			"User4": false, "User5": false,
		}

		for metric := range metricCh {
			totalMetrics++
			dto := &dto.Metric{}
			err := metric.Write(dto)
			if err != nil {
				t.Fatalf("Failed to write metric: %v", err)
			}

			desc := metric.Desc().String()

			if strings.Contains(desc, "plays_total") {
				playMetrics++
				// Check for expected users in labels
				for _, label := range dto.Label {
					if *label.Name == "user" {
						if _, exists := expectedUsers[*label.Value]; exists {
							expectedUsers[*label.Value] = true
						}
					}
				}
			} else if strings.Contains(desc, "play_seconds_total") {
				durationMetrics++
			}
		}

		t.Logf("Integration test completed: %d total metrics, %d play metrics, %d duration metrics", totalMetrics, playMetrics, durationMetrics)

		// Verify we have metrics
		if playMetrics == 0 {
			t.Error("Expected to find plays_total metrics")
		}
		if durationMetrics == 0 {
			t.Error("Expected to find play_seconds_total metrics")
		}

		// Verify all expected users were found
		for user, found := range expectedUsers {
			if !found {
				t.Errorf("Expected user %s not found in metrics", user)
			}
		}
	})

	t.Run("DeviceTypeVariety", func(t *testing.T) {
		// Test that we have the variety of devices from real data
		expectedDevices := []string{"OSX", "VizioTV", "RokuTV001", "Apple TV"}
		expectedProducts := []string{"Plex Web", "Plex for Vizio", "Plex for Roku", "Plex for Apple TV"}

		sess.mtx.Lock()
		devices := make(map[string]bool)
		products := make(map[string]bool)

		for _, session := range sess.sessions {
			devices[session.session.Player.Platform] = true
			products[session.session.Player.Product] = true
		}
		sess.mtx.Unlock()

		for _, device := range expectedDevices {
			if !devices[device] {
				t.Errorf("Expected device %s not found in sessions", device)
			}
		}

		for _, product := range expectedProducts {
			if !products[product] {
				t.Errorf("Expected product %s not found in sessions", product)
			}
		}
	})

	t.Run("TranscodeScenarios", func(t *testing.T) {
		// Test different transcode scenarios from data
		testCases := []struct {
			sessionID         string
			expectedTranscode string
		}{
			{"328", "both"}, // Wizard of Oz - OSX Web transcoding both
			{"329", "none"}, // Back to the Future - Vizio direct play
			{"330", "both"}, // The Goonies - OSX Web transcoding
			{"335", "both"}, // The Karate Kid - Roku transcoding
			{"336", "both"}, // Batman Begins - Apple TV transcoding
		}

		sess.mtx.Lock()
		for _, tc := range testCases {
			session, exists := sess.sessions[tc.sessionID]
			if !exists {
				t.Errorf("Session %s not found", tc.sessionID)
				continue
			}

			if session.transcodeType != tc.expectedTranscode {
				t.Errorf("Session %s: expected transcode %s, got %s", tc.sessionID, tc.expectedTranscode, session.transcodeType)
			}
		}
		sess.mtx.Unlock()
	})

	t.Run("UserDistribution", func(t *testing.T) {
		// Verify we have the expected users from real data
		expectedUsers := []string{"User1", "User2", "User3", "User4", "User5"}

		sess.mtx.Lock()
		users := make(map[string]bool)
		for _, session := range sess.sessions {
			users[session.session.User.Title] = true
		}
		sess.mtx.Unlock()

		for _, user := range expectedUsers {
			if !users[user] {
				t.Errorf("Expected user %s not found in sessions", user)
			}
		}

		if len(users) != len(expectedUsers) {
			t.Errorf("Expected %d users, got %d", len(expectedUsers), len(users))
		}
	})
}

func TestTranscodeKind_RealWorldScenarios(t *testing.T) {
	// Test cases based on actual Prometheus data patterns
	testCases := []struct {
		name     string
		session  ttPlex.TranscodeSession
		expected string
	}{
		{
			name: "Wizard_of_Oz_Both_Transcode",
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
			name: "Back_to_the_Future_DirectPlay",
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
			name: "Apple_TV_Both_Transcode",
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
			name: "Roku_Video_Only_Transcode",
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
