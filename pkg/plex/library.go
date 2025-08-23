package plex

type Library struct {
	Name string
	ID   string
	Type string

	Server *Server

	DurationTotal int64
	StorageTotal  int64
	ItemsCount    int64
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
