package plex

type Library struct {
	Name string
	ID   string
	Type string

	Server *Server

	DurationTotal int64
	StorageTotal  int64
	ItemsCount    int64
	// cachedTrackType records which "type" parameter worked for this
	// music library ("7" or "10"). Empty if unknown.
	cachedTrackType string
	// lastMusicFetch stores unix seconds of the last time we fetched
	// track counts for this library. Used to avoid repeating expensive
	// queries too often.
	lastMusicFetch int64
	// lastMusicCount stores the last successful exact track count we fetched.
	// If non-zero we can reuse it when skipping a fetch.
	lastMusicCount int64
	// lastEpisodeFetch stores unix seconds of the last time we fetched
	// episode counts for this library (shows). Used to avoid repeating
	// expensive queries too often.
	lastEpisodeFetch int64
	// lastEpisodeCount stores the last successful exact episode count.
	lastEpisodeCount int64
}

func isLibraryDirectoryType(directoryType string) bool {
	switch directoryType {
	case
		"movie",
		"show",
		"artist",
		"music",
		"photo",
		"homevideo":
		return true
	}
	return false
}
