package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	serverLabels = []string{
		"server_type", // Backend type: plex
		"server",      // Server friendly name
		"server_id",   // Server unique id
	}

	libraryLabels = append(append([]string(nil), serverLabels...),
		"library_type", // movie, show, or artist ?
		"library",      // Library friendly name
		"library_id",   // Library unique id
	)

	playLabels = append(append([]string(nil), libraryLabels...),
		"media_type",             // Movies, tv_shows, music, or live_tv
		"title",                  // For tv shows this is the series title. For music this is the artist.
		"child_title",            // For tv shows this is the season title. For music this is the album title.
		"grandchild_title",       // For tv shows this is the episode title. For music this is the track title.
		"stream_type",            // DirectPlay, DirectStream, or transcode
		"stream_resolution",      // Destination resolution
		"stream_file_resolution", // Source resolution
		"stream_bitrate",         //
		"device",                 // Device friendly name
		"device_type",            //
		"user",                   // User name
		"session",
		"transcode_type",
	)

	ServerInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "server_info",
	}, append(append([]string(nil), serverLabels...), "version", "platform", "platform_version"))

	ServerHostCpuUtilization = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "host_cpu_util",
	}, serverLabels)

	ServerHostMemUtilization = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "host_mem_util",
	}, serverLabels)

	MetricsLibraryDurationTotalDesc = prometheus.NewDesc(
		"library_duration_total",
		"Total duration of a library in ms",
		libraryLabels,
		nil,
	)

	MetricsLibraryStorageTotalDesc = prometheus.NewDesc(
		"library_storage_total",
		"Total storage size of a library in Bytes",
		libraryLabels,
		nil,
	)

	// plex_library_items is a gauge representing the instantaneous number of
	// items contained in a library (section). Use a plain gauge (no _total)
	// to follow Prometheus conventions for instantaneous sizes.
	MetricsLibraryItemsDesc = prometheus.NewDesc(
		"plex_library_items",
		"Number of items in a library section",
		libraryLabels,
		nil,
	)

	MetricPlayCountDesc = prometheus.NewDesc(
		"plays_total",
		"Total play counts",
		playLabels,
		nil,
	)

	MetricPlaySecondsTotalDesc = prometheus.NewDesc(
		"play_seconds_total",
		"Total play time per session",
		playLabels,
		nil,
	)

	MetricEstimatedTransmittedBytesTotal = prometheus.NewDesc(
		"estimated_transmit_bytes_total",
		"Total estimated bytes transmitted",
		serverLabels, nil)

	MetricTransmittedBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "transmit_bytes_total",
	}, serverLabels)

	// plex_media_movies / plex_media_episodes are gauges with server-level labels
	MetricsMediaMoviesDesc = prometheus.NewDesc(
		"plex_media_movies",
		"Total number of movie items across all libraries for a server",
		serverLabels,
		nil,
	)

	MetricsMediaEpisodesDesc = prometheus.NewDesc(
		"plex_media_episodes",
		"Total number of episode items across all libraries for a server",
		serverLabels,
		nil,
	)

	MetricsMediaMusicDesc = prometheus.NewDesc(
		"plex_media_music",
		"Total number of music items (tracks) across all libraries for a server",
		serverLabels,
		nil,
	)

	MetricsMediaPhotosDesc = prometheus.NewDesc(
		"plex_media_photos",
		"Total number of photo items across all libraries for a server",
		serverLabels,
		nil,
	)

	MetricsMediaOtherVideosDesc = prometheus.NewDesc(
		"plex_media_other_videos",
		"Total number of other video items (home videos) across all libraries for a server",
		serverLabels,
		nil,
	)
)

func Register(collectors ...prometheus.Collector) {
	prometheus.MustRegister(collectors...)
}

func LibraryDuration(value int64,
	serverType, serverName, serverID,
	libraryType, libraryName, libraryID string,
) prometheus.Metric {

	return prometheus.MustNewConstMetric(MetricsLibraryDurationTotalDesc,
		prometheus.GaugeValue,
		float64(value),
		serverType, serverName, serverID,
		libraryType, libraryName, libraryID,
	)
}

func LibraryStorage(value int64,
	serverType, serverName, serverID,
	libraryType, libraryName, libraryID string,
) prometheus.Metric {

	return prometheus.MustNewConstMetric(MetricsLibraryStorageTotalDesc,
		prometheus.GaugeValue,
		float64(value),
		serverType, serverName, serverID,
		libraryType, libraryName, libraryID,
	)
}

func LibraryItems(value int64,
	serverType, serverName, serverID,
	libraryType, libraryName, libraryID string,
) prometheus.Metric {

	return prometheus.MustNewConstMetric(MetricsLibraryItemsDesc,
		prometheus.GaugeValue,
		float64(value),
		serverType, serverName, serverID,
		libraryType, libraryName, libraryID,
	)
}

func MediaMovies(value int64, serverType, serverName, serverID string) prometheus.Metric {
	return prometheus.MustNewConstMetric(MetricsMediaMoviesDesc, prometheus.GaugeValue, float64(value), serverType, serverName, serverID)
}

func MediaEpisodes(value int64, serverType, serverName, serverID string) prometheus.Metric {
	return prometheus.MustNewConstMetric(MetricsMediaEpisodesDesc, prometheus.GaugeValue, float64(value), serverType, serverName, serverID)
}

func MediaMusic(value int64, serverType, serverName, serverID string) prometheus.Metric {
	return prometheus.MustNewConstMetric(MetricsMediaMusicDesc, prometheus.GaugeValue, float64(value), serverType, serverName, serverID)
}

func MediaPhotos(value int64, serverType, serverName, serverID string) prometheus.Metric {
	return prometheus.MustNewConstMetric(MetricsMediaPhotosDesc, prometheus.GaugeValue, float64(value), serverType, serverName, serverID)
}

func MediaOtherVideos(value int64, serverType, serverName, serverID string) prometheus.Metric {
	return prometheus.MustNewConstMetric(MetricsMediaOtherVideosDesc, prometheus.GaugeValue, float64(value), serverType, serverName, serverID)
}

func Play(value float64, serverType, serverName, serverID,
	library, libraryID, libraryType,
	mediaType,
	title, childTitle, grandchildTitle,
	streamType, streamResolution, streamFileResolution, streamBitrate,
	device, deviceType,
	user, session, transcodeType string,
) prometheus.Metric {

	// the last label is transcode_type; callers should pass a value such as
	// "audio", "video", "both", or "unknown".
	return prometheus.MustNewConstMetric(MetricPlayCountDesc,
		prometheus.CounterValue,
		value,
		serverType, serverName, serverID,
		library, libraryID, libraryType,
		mediaType,
		title, childTitle, grandchildTitle,
		streamType, streamResolution, streamFileResolution, streamBitrate,
		device, deviceType,
		user, session, transcodeType,
	)
}

func PlayDuration(value float64, serverType, serverName, serverID,
	library, libraryID, libraryType,
	mediaType,
	title, childTitle, grandchildTitle,
	streamType, streamResolution, streamFileResolution, streamBitrate,
	device, deviceType,
	user, session, transcodeType string,
) prometheus.Metric {

	// last label is transcode_type; default to unknown here and let callers
	// update if they can determine the type.
	return prometheus.MustNewConstMetric(MetricPlaySecondsTotalDesc,
		prometheus.CounterValue,
		value,
		serverType, serverName, serverID,
		library, libraryID, libraryType,
		mediaType,
		title, childTitle, grandchildTitle,
		streamType, streamResolution, streamFileResolution, streamBitrate,
		device, deviceType,
		user, session, transcodeType,
	)
}
