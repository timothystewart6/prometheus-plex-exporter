package plex

import (
	"log"
	"net/url"
	"os"
	"sort"
	"strconv"
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
	// LibraryRefreshInterval controls how often we re-query expensive per-library
	// counts (music tracks, episodes). If zero, defaults to 1h.
	LibraryRefreshInterval time.Duration
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

	// Configure library refresh interval from environment variable
	// LIBRARY_REFRESH_INTERVAL must be integer minutes (e.g. "15" for 15 minutes).
	// If unset, default to 15 minutes. If explicitly set to 0, caching is disabled.
	if v := os.Getenv("LIBRARY_REFRESH_INTERVAL"); v != "" {
		if mins, err := strconv.Atoi(v); err == nil && mins >= 0 {
			server.LibraryRefreshInterval = time.Duration(mins) * time.Minute
		} else {
			log.Printf("invalid LIBRARY_REFRESH_INTERVAL %q; expected integer minutes (e.g. 15); using default", v)
		}
	} else {
		// default to 15 minutes when not set
		server.LibraryRefreshInterval = 15 * time.Minute
	}

	// Log effective interval; if 0 then caching is disabled
	if server.LibraryRefreshInterval == 0 {
		log.Printf("Library refresh interval disabled; caching is off")
	} else {
		log.Printf("Library refresh interval set to %d minutes", int(server.LibraryRefreshInterval.Minutes()))
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
				// Check existing server cache for recent items count to avoid the expensive call.
				// If LibraryRefreshInterval == 0 then caching is disabled and we always fetch.
				usedCache := false
				if s.LibraryRefreshInterval > 0 {
					interval := s.LibraryRefreshInterval
					s.mtx.Lock()
					for _, old := range s.libraries {
						if old.ID == lib.ID {
							if old.lastItemsFetch != 0 && time.Now().Unix()-old.lastItemsFetch < int64(interval.Seconds()) {
								lib.ItemsCount = old.lastItemsCount
								lib.lastItemsCount = old.lastItemsCount
								lib.lastItemsFetch = old.lastItemsFetch
								usedCache = true
							}
							break
						}
					}
					s.mtx.Unlock()
				}

				path := "/library/sections/" + lib.ID + "/all"
				if usedCache {
					log.Printf("Using cached items count for library %s (ID: %s): %d", lib.Name, lib.ID, lib.ItemsCount)
				} else {
					log.Printf("Fetching items for library %s (ID: %s, type: %s) from path: %s", lib.Name, lib.ID, lib.Type, path)
					if err := s.Client.Get(path, &results); err == nil {
						lib.ItemsCount = int64(results.MediaContainer.Size)
						if s.LibraryRefreshInterval > 0 {
							lib.lastItemsCount = lib.ItemsCount
							lib.lastItemsFetch = time.Now().Unix()
						}
						log.Printf("Library %s (ID: %s) has %d items", lib.Name, lib.ID, results.MediaContainer.Size)
					} else {
						log.Printf("Error fetching items for library %s (ID: %s): %v", lib.Name, lib.ID, err)
					}
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
		case "music", "artist":
			// Some Plex servers report music libraries as "artist" rather than "music"
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
	log.Printf("Starting library processing for %d libraries; pre-parallel totals: movies=%d episodes=%d music=%d photos=%d other=%d", len(newLibraries), moviesTotal, episodesTotal, musicTotal, photosTotal, otherVideosTotal)
	for _, lib := range newLibraries {
		if lib.Type == "show" {
			wg.Add(1)
			sem <- struct{}{}
			go func(sectionID string) {
				defer wg.Done()
				defer func() { <-sem }()
				var results ttPlex.SearchResults
				// Check library cache to decide whether to skip fetching. If LibraryRefreshInterval == 0
				// caching is disabled and we always fetch.
				useCached := false
				interval := s.LibraryRefreshInterval
				if s.LibraryRefreshInterval > 0 {
					s.mtx.Lock()
					for _, l := range s.libraries {
						if l.ID == sectionID {
							if l.lastEpisodeFetch != 0 && time.Now().Unix()-l.lastEpisodeFetch < int64(interval.Seconds()) {
								useCached = true
							}
							break
						}
					}
					s.mtx.Unlock()
				}

				if useCached {
					log.Printf("Skipping episode fetch for section %s; cached within %s", sectionID, interval)
					s.mtx.Lock()
					for _, l := range s.libraries {
						if l.ID == sectionID {
							mu.Lock()
							episodesTotal += l.lastEpisodeCount
							mu.Unlock()
							break
						}
					}
					s.mtx.Unlock()
				} else {
					path := "/library/sections/" + sectionID + "/all?type=4"
					log.Printf("Fetching episodes for show library ID: %s from path: %s", sectionID, path)
					if err := s.Client.Get(path, &results); err == nil {
						mu.Lock()
						episodesTotal += int64(results.MediaContainer.Size)
						mu.Unlock()

						// update library cache only when caching enabled
						if s.LibraryRefreshInterval > 0 {
							s.mtx.Lock()
							for _, l := range s.libraries {
								if l.ID == sectionID {
									l.lastEpisodeCount = int64(results.MediaContainer.Size)
									l.lastEpisodeFetch = time.Now().Unix()
									break
								}
							}
							s.mtx.Unlock()
						}
					} else {
						log.Printf("Error fetching episodes for section %s: %v", sectionID, err)
					}
				}
			}(lib.ID)
		}

		if lib.Type == "music" || lib.Type == "artist" {
			log.Printf("Found music library: %s (ID: %s, type: %s)", lib.Name, lib.ID, lib.Type)
			wg.Add(1)
			sem <- struct{}{}
			go func(sectionID string) {
				defer wg.Done()
				defer func() { <-sem }()
				var results ttPlex.SearchResults
				var trackCount int64

				// Check cache: if LibraryRefreshInterval == 0 caching is disabled.
				interval := s.LibraryRefreshInterval
				useCached := false
				if s.LibraryRefreshInterval > 0 {
					s.mtx.Lock()
					// find library in s.libraries and inspect its cache fields
					for _, l := range s.libraries {
						if l.ID == sectionID {
							if l.lastMusicFetch != 0 && time.Now().Unix()-l.lastMusicFetch < int64(interval.Seconds()) {
								useCached = true
							}
							break
						}
					}
					s.mtx.Unlock()
				}

				if useCached {
					if s.LibraryRefreshInterval > 0 {
						log.Printf("Skipping music fetch for section %s; cached within %s", sectionID, interval)
					} else {
						log.Printf("Skipping music fetch for section %s; caching is disabled, but using last known value if present", sectionID)
					}
					// use the last successful exact track count if available,
					// otherwise fall back to the unfiltered ItemsCount.
					s.mtx.Lock()
					for _, l := range s.libraries {
						if l.ID == sectionID {
							if l.lastMusicCount != 0 {
								trackCount = l.lastMusicCount
							} else {
								trackCount = l.ItemsCount
							}
							break
						}
					}
					s.mtx.Unlock()
				} else {
					// Prefer type=10 (some servers) then fallback to type=7
					for _, trackType := range []string{"10", "7"} {
						path := "/library/sections/" + sectionID + "/all?type=" + trackType
						log.Printf("Fetching music tracks from path: %s", path)
						if err := s.Client.Get(path, &results); err == nil {
							if results.MediaContainer.Size > 0 {
								log.Printf("Successfully fetched %d music tracks using type=%s from section %s", results.MediaContainer.Size, trackType, sectionID)
								trackCount = int64(results.MediaContainer.Size)
								// update library cache with successful type and timestamp
								s.mtx.Lock()
								for _, l := range s.libraries {
									if l.ID == sectionID {
												l.cachedTrackType = trackType
												if s.LibraryRefreshInterval > 0 {
													l.lastMusicFetch = time.Now().Unix()
													l.lastMusicCount = int64(results.MediaContainer.Size)
												}
										break
									}
								}
								s.mtx.Unlock()
								break
							} else {
								log.Printf("No tracks found using type=%s for section %s", trackType, sectionID)
							}
						} else {
							log.Printf("Error fetching music tracks using type=%s for section %s: %v", trackType, sectionID, err)
						}
					}
				}

				mu.Lock()
				musicTotal += trackCount
				mu.Unlock()
			}(lib.ID)
		}
	}
	wg.Wait()
	close(sem)

	log.Printf("Final music count: %d", musicTotal)

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
