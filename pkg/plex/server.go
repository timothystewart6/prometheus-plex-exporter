package plex

import (
	"log"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/grafana/plexporter/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"

	ttPlex "github.com/timothystewart6/go-plex-client"
)

// Plex API type parameter mappings:
// 1=movie, 2=show, 3=season, 4=episode, 5=artist, 6=album, 7=track, 8=photoalbum, 9=photo, 10=track
// Note: Some Plex servers use type=10 for tracks instead of type=7

// getContentTypeForLibrary returns a descriptive label for what type of items
// are counted in each library type based on what the Plex API returns
func getContentTypeForLibrary(libraryType string) string {
	switch libraryType {
	case "movie":
		return "movies"
	case "show":
		return "shows"
	case "artist":
		return "artists"
	case "photo":
		return "photos"
	default:
		return "items"
	}
}

type Server struct {
	ID      string
	Name    string
	Version string

	Token string
	URL   *url.URL

	Client *Client

	listener *plexListener

	mtx       sync.Mutex
	libraries []*Library

	lastBandwidthAt int
	MovieCount      int64
	EpisodeCount    int64
	MusicCount      int64
	PhotoCount      int64
	OtherVideoCount int64
}

type StatisticsBandwidth struct {
	At    int   `json:"at"`
	Lan   bool  `json:"lan"`
	Bytes int64 `json:"bytes"`
}

type StatisticsResources struct {
	At             int     `json:"at"`
	HostCpuUtil    float64 `json:"hostCpuUtilization"`
	HostMemUtil    float64 `json:"hostMemoryUtilization"`
	ProcessCpuUtil float64 `json:"processCpuUtilization"`
	ProcessMemUtil float64 `json:"processMemoryUtilization"`
}

func NewServer(serverURL, token string) (*Server, error) {
	client, err := NewClient(serverURL, token)
	if err != nil {
		return nil, err
	}

	server := &Server{
		URL:   client.URL,
		Token: client.Token,

		Client:          client,
		lastBandwidthAt: int(time.Now().Unix()),
	}

	err = server.Refresh()
	if err != nil {
		return nil, err
	}

	ticker := time.NewTicker(time.Second * 5)
	go func() {
		for range ticker.C {
			_ = server.Refresh()
		}
	}()

	return server, nil
}

func (s *Server) Refresh() error {
	// Avoid holding s.mtx across network calls to prevent blocking the
	// metrics scrape path. Build the refreshed state locally then copy it
	// into the server struct under lock.
	container := struct {
		MediaContainer struct {
			FriendlyName      string `json:"friendlyName"`
			MachineIdentifier string `json:"machineIdentifier"`
			Version           string `json:"version"`
			MediaProviders    []struct {
				Identifier string `json:"identifier"`
				Features   []struct {
					Type        string `json:"type"`
					Directories []struct {
						Identifier    string `json:"id"`
						DurationTotal int64  `json:"durationTotal"`
						StorageTotal  int64  `json:"storageTotal"`
						Title         string `json:"title"`
						Type          string `json:"type"`
					} `json:"Directory"`
				} `json:"Feature"`
			} `json:"MediaProvider"`
		} `json:"MediaContainer"`
	}{}
	err := s.Client.Get("/media/providers?includeStorage=1", &container)
	if err != nil {
		return err
	}
	// Build new library list and totals without holding the server lock.
	var newLibraries []*Library
	for _, provider := range container.MediaContainer.MediaProviders {
		if provider.Identifier != "com.plexapp.plugins.library" {
			continue
		}
		for _, feature := range provider.Features {
			if feature.Type != "content" {
				continue
			}
			for _, directory := range feature.Directories {
				if !isLibraryDirectoryType(directory.Type) {
					continue
				}
				lib := &Library{
					ID:            directory.Identifier,
					Name:          directory.Title,
					Type:          directory.Type,
					DurationTotal: directory.DurationTotal,
					StorageTotal:  directory.StorageTotal,
					Server:        s,
				}

				var results ttPlex.SearchResults
				path := "/library/sections/" + lib.ID + "/all"
				if err := s.Client.Get(path, &results); err == nil {
					lib.ItemsCount = int64(results.MediaContainer.Size)
				}

				newLibraries = append(newLibraries, lib)
			}
		}
	}

	// Compute server-level totals for movies and episodes. Parallelize
	// episode/music track queries but accumulate into locals first.
	var moviesTotal int64
	var episodesTotal int64
	var musicTotal int64
	var photosTotal int64
	var otherVideosTotal int64

	for _, lib := range newLibraries {
		switch lib.Type {
		case "movie":
			moviesTotal += lib.ItemsCount
		case "music":
			musicTotal += lib.ItemsCount
		case "photo":
			photosTotal += lib.ItemsCount
		case "homevideo":
			otherVideosTotal += lib.ItemsCount
		}
	}

	// Plex API media type parameters:
	// 1 = movie, 2 = show, 3 = season, 4 = episode, 5 = artist, 6 = album, 7 = track, 8 = photoalbum, 9 = photo
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	
	// Reset musicTotal to 0 since we'll be counting tracks, not artists
	musicTotal = 0
	for _, lib := range newLibraries {
		if lib.Type == "show" {
			wg.Add(1)
			sem <- struct{}{}
			go func(sectionID string) {
				defer wg.Done()
				defer func() { <-sem }()
				var results ttPlex.SearchResults
				path := "/library/sections/" + sectionID + "/all?type=4"
				if err := s.Client.Get(path, &results); err == nil {
					mu.Lock()
					episodesTotal += int64(results.MediaContainer.Size)
					mu.Unlock()
				}
			}(lib.ID)
		}

		if lib.Type == "music" {
			wg.Add(1)
			sem <- struct{}{}
			go func(sectionID string) {
				defer wg.Done()
				defer func() { <-sem }()
				var results ttPlex.SearchResults
				path := "/library/sections/" + sectionID + "/all?type=10"
				if err := s.Client.Get(path, &results); err == nil {
					mu.Lock()
					musicTotal += int64(results.MediaContainer.Size)
					mu.Unlock()
				} else {
					// Log the error to help debug the issue
					log.Printf("Error fetching music tracks for section %s: %v", sectionID, err)
				}
			}(lib.ID)
		}
	}
	wg.Wait()
	close(sem)

	// Update server state under lock.
	s.mtx.Lock()
	s.ID = container.MediaContainer.MachineIdentifier
	s.Name = container.MediaContainer.FriendlyName
	s.Version = container.MediaContainer.Version
	s.libraries = newLibraries
	s.MovieCount = moviesTotal
	s.EpisodeCount = episodesTotal
	s.MusicCount = musicTotal
	s.PhotoCount = photosTotal
	s.OtherVideoCount = otherVideosTotal
	s.mtx.Unlock()

	if err := s.refreshServerInfo(); err != nil {
		return err
	}
	if err := s.refreshResources(); err != nil {
		return err
	}
	if err := s.refreshBandwidth(); err != nil {
		return err
	}

	return nil
}

func (s *Server) refreshServerInfo() error {
	resp := struct {
		MediaContainer struct {
			Version         string `json:"version"`
			Platform        string `json:"platform"`
			PlatformVersion string `json:"platformVersion"`
		} `json:"MediaContainer"`
	}{}
	err := s.Client.Get("/", &resp)

	if err != nil {
		return err
	}

	metrics.ServerInfo.WithLabelValues("plex", s.Name, s.ID, resp.MediaContainer.Version, resp.MediaContainer.Platform, resp.MediaContainer.PlatformVersion).Set(1.0)

	return nil
}

func (s *Server) refreshResources() error {
	resources := struct {
		MediaContainer struct {
			StatisticsResources []StatisticsResources `json:"StatisticsResources"`
		} `json:"MediaContainer"`
	}{}
	err := s.Client.Get("/statistics/resources?timespan=6", &resources)

	// This is a paid feature and API may not be available
	if err == ErrNotFound {
		return nil
	}

	if err != nil {
		return err
	}

	if len(resources.MediaContainer.StatisticsResources) > 0 {
		// The last entry is the most recent
		i := len(resources.MediaContainer.StatisticsResources) - 1
		stats := resources.MediaContainer.StatisticsResources[i]

		metrics.ServerHostCpuUtilization.WithLabelValues("plex", s.Name, s.ID).Set(stats.HostCpuUtil)
		metrics.ServerHostMemUtilization.WithLabelValues("plex", s.Name, s.ID).Set(stats.HostMemUtil)
	}

	return nil
}

func (s *Server) refreshBandwidth() error {
	bandwidth := struct {
		MediaContainer struct {
			StatisticsBandwith []StatisticsBandwidth `json:"StatisticsBandwidth"`
		} `json:"MediaContainer"`
	}{}
	err := s.Client.Get("/statistics/bandwidth?timespan=6", &bandwidth)

	// This is a paid feature and API may not be available
	if err == ErrNotFound {
		return nil
	}

	if err != nil {
		return err
	}

	// Record updates newer than our last sync.  We also keep track of
	// the highest timestamp see and use that as our last sync time.
	// Sort by timestamp to ensure they are processed in order
	updates := bandwidth.MediaContainer.StatisticsBandwith

	sort.Slice(updates, func(i, j int) bool {
		return updates[i].At < updates[j].At
	})

	highest := 0
	for _, u := range updates {
		if u.At > s.lastBandwidthAt {
			metrics.MetricTransmittedBytesTotal.WithLabelValues("plex", s.Name, s.ID).Add(float64(u.Bytes))

			if u.At > highest {
				highest = u.At
			}
		}
	}

	s.lastBandwidthAt = highest

	return nil
}

func (s *Server) Library(id string) *Library {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, library := range s.libraries {
		if library.ID == id {
			return library
		}
	}
	return nil
}

func (s *Server) Describe(ch chan<- *prometheus.Desc) {
	ch <- metrics.MetricsLibraryDurationTotalDesc
	ch <- metrics.MetricsLibraryStorageTotalDesc
	ch <- metrics.MetricsLibraryItemsDesc
	ch <- metrics.MetricsMediaMoviesDesc
	ch <- metrics.MetricsMediaEpisodesDesc
	ch <- metrics.MetricsMediaMusicDesc
	ch <- metrics.MetricsMediaPhotosDesc
	ch <- metrics.MetricsMediaOtherVideosDesc

	if s.listener != nil {
		s.listener.activeSessions.Describe(ch)
	}
}

func (s *Server) Collect(ch chan<- prometheus.Metric) {
	s.mtx.Lock()

	for _, library := range s.libraries {
		ch <- metrics.LibraryDuration(library.DurationTotal,
			"plex",
			library.Server.Name,
			library.Server.ID,
			library.Type,
			library.Name,
			library.ID,
		)
		ch <- metrics.LibraryStorage(library.StorageTotal,
			"plex",
			library.Server.Name,
			library.Server.ID,
			library.Type,
			library.Name,
			library.ID,
		)

		// Determine what type of content is being counted based on library type
		contentType := getContentTypeForLibrary(library.Type)

		ch <- metrics.LibraryItems(library.ItemsCount,
			"plex",
			library.Server.Name,
			library.Server.ID,
			library.Type,
			library.Name,
			library.ID,
			contentType,
		)
	}

	// HACK: Unlock prior to asking sessions to collect since it fetches
	// 			 libraries by ID, which locks the server mutex
	s.mtx.Unlock()

	// Emit server-level media totals
	ch <- metrics.MediaMovies(s.MovieCount, "plex", s.Name, s.ID)
	ch <- metrics.MediaEpisodes(s.EpisodeCount, "plex", s.Name, s.ID)
	ch <- metrics.MediaMusic(s.MusicCount, "plex", s.Name, s.ID)
	ch <- metrics.MediaPhotos(s.PhotoCount, "plex", s.Name, s.ID)
	ch <- metrics.MediaOtherVideos(s.OtherVideoCount, "plex", s.Name, s.ID)

	if s.listener != nil {
		s.listener.activeSessions.Collect(ch)
	}
}
