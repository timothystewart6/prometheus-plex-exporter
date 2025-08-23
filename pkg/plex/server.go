package plex

import (
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/grafana/plexporter/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"

	jrplex "github.com/jrudio/go-plex-client"
)

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
	s.mtx.Lock()
	defer s.mtx.Unlock()

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

	s.ID = container.MediaContainer.MachineIdentifier
	s.Name = container.MediaContainer.FriendlyName
	s.Version = container.MediaContainer.Version
	s.libraries = nil
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

				// Attempt to fetch the library content to obtain an
				// instantaneous item count. Use our local Client to GET
				// the library sections contents and decode into the
				// vendored SearchResults type which exposes
				// MediaContainer.Size for the count. If the call fails
				// we'll skip setting ItemsCount to avoid hard failures.
				var results jrplex.SearchResults
				path := "/library/sections/" + lib.ID + "/all"
				if err := s.Client.Get(path, &results); err == nil {
					lib.ItemsCount = int64(results.MediaContainer.Size)
				}

				s.libraries = append(s.libraries, lib)
			}
		}
	}

	// Compute server-level totals for movies and episodes.
	// Reuse the previously fetched library ItemsCount where possible to
	// avoid duplicate requests. For 'movie' libraries, ItemsCount already
	// reflects the number of movies in the section (from /all). For 'show'
	// libraries we need to query with type=4 to count episodes.
	var moviesTotal int64
	var episodesTotal int64
	var musicTotal int64
	var photosTotal int64
	var otherVideosTotal int64

	// Sum movie library counts locally (ItemsCount already fetched).
	for _, lib := range s.libraries {
		switch lib.Type {
		case "movie":
			moviesTotal += lib.ItemsCount
		case "music":
			// music libraries report albums in /all; we want track counts so
			// query type=10 (track) where necessary later. For now use ItemsCount.
			musicTotal += lib.ItemsCount
		case "photo":
			photosTotal += lib.ItemsCount
		case "homevideo":
			otherVideosTotal += lib.ItemsCount
		}
	}

	// For show libraries, do episode counts in parallel to reduce latency.
	// Limit concurrency with a semaphore to avoid overloading the Plex server.
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // max 5 concurrent requests
	for _, lib := range s.libraries {
		if lib.Type == "show" {
			wg.Add(1)
			sem <- struct{}{}
			go func(sectionID string) {
				defer wg.Done()
				defer func() { <-sem }()
				var results jrplex.SearchResults
				path := "/library/sections/" + sectionID + "/all?type=4"
				if err := s.Client.Get(path, &results); err == nil {
					mu.Lock()
					episodesTotal += int64(results.MediaContainer.Size)
					mu.Unlock()
				}
			}(lib.ID)
		}

		// For music, we prefer track counts (type=10). Do additional requests
		// in parallel similar to shows.
		if lib.Type == "music" {
			wg.Add(1)
			sem <- struct{}{}
			go func(sectionID string) {
				defer wg.Done()
				defer func() { <-sem }()
				var results jrplex.SearchResults
				path := "/library/sections/" + sectionID + "/all?type=10"
				if err := s.Client.Get(path, &results); err == nil {
					mu.Lock()
					musicTotal += int64(results.MediaContainer.Size)
					mu.Unlock()
				}
			}(lib.ID)
		}

		// Other library types (photo, homevideo) usually have ItemsCount already
		// populated from the initial /all call, so no extra requests are needed.
	}
	wg.Wait()
	close(sem)

	s.MovieCount = moviesTotal
	s.EpisodeCount = episodesTotal
	s.MusicCount = musicTotal
	s.PhotoCount = photosTotal
	s.OtherVideoCount = otherVideosTotal

	err = s.refreshServerInfo()
	if err != nil {
		return err
	}

	err = s.refreshResources()
	if err != nil {
		return err
	}

	err = s.refreshBandwidth()
	if err != nil {
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

		ch <- metrics.LibraryItems(library.ItemsCount,
			"plex",
			library.Server.Name,
			library.Server.ID,
			library.Type,
			library.Name,
			library.ID,
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
