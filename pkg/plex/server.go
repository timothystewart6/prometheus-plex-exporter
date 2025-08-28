package plex

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	kitlog "github.com/go-kit/log"
	"github.com/go-kit/log/level"

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
	// Debug enables verbose debug logging when true.
	Debug bool

	// fullRefreshDone is closed once the initial background full refresh
	// (deferred at startup) completes. Tests may wait on this to observe
	// when the heavy startup work has finished.
	fullRefreshDone chan struct{}
}

// pkg-level logger used for structured logs within this package. Tests and
// callers may still pass their own logger to listeners; this logger is a
// sensible default for package-level messages.
var pkgLog = kitlog.NewLogfmtLogger(kitlog.NewSyncWriter(os.Stderr))

// debugf logs only when server debug is enabled.
func (s *Server) debugf(format string, args ...interface{}) {
	if s != nil && s.Debug {
		_ = level.Debug(pkgLog).Log("msg", fmt.Sprintf(format, args...))
	}
}

// Controls for startup deferred full refresh. Defaults are production
// values; tests in this package may override them to avoid long sleeps.
var (
	startupFullRefreshDelaySeconds = 15
	startupFullRefreshJitterMax    = 5
)

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
			_ = level.Warn(pkgLog).Log("msg", "invalid LIBRARY_REFRESH_INTERVAL", "value", v, "note", "expected integer minutes (e.g. 15); falling back to default 15 minutes")
		}
	} else {
		// default to 15 minutes when not set
		server.LibraryRefreshInterval = 15 * time.Minute
	}

	// Configure debug logging via DEBUG ("1" or "true")
	if v := os.Getenv("DEBUG"); v == "1" || v == "true" {
		server.Debug = true
	}

	// Log effective interval; if 0 then caching is disabled
	if server.LibraryRefreshInterval == 0 {
		_ = level.Info(pkgLog).Log("msg", "Library refresh interval disabled; caching is off")
	} else {
		_ = level.Info(pkgLog).Log("msg", "Library refresh interval set", "minutes", int(server.LibraryRefreshInterval.Minutes()))
	}

	// Perform a fast, lightweight refresh at startup to populate basic
	// server and library metadata without running expensive per-library
	// item counts. Defer the expensive full refresh to run in the
	// background after a short delay to reduce startup memory/CPU spikes.
	if err := server.RefreshLight(); err != nil {
		return nil, err
	}

	// Channel closed when the initial background full refresh completes.
	server.fullRefreshDone = make(chan struct{})

	// Schedule the full refresh after 15s + small jitter.
	go func() {
		delaySec := startupFullRefreshDelaySeconds
		// Use crypto/rand for jitter to satisfy gosec's recommendation
		jitter := 0
		if startupFullRefreshJitterMax > 0 {
			if n, err := rand.Int(rand.Reader, big.NewInt(int64(startupFullRefreshJitterMax))); err == nil {
				jitter = int(n.Int64())
			}
		}
		time.Sleep(time.Duration(delaySec+jitter) * time.Second)

		_ = level.Info(pkgLog).Log("msg", "Starting background full library refresh")
		if err := server.Refresh(); err != nil {
			_ = level.Error(pkgLog).Log("msg", "background full refresh failed", "err", err)
		} else {
			_ = level.Info(pkgLog).Log("msg", "background full library refresh completed")
		}
		close(server.fullRefreshDone)
	}()

	ticker := time.NewTicker(time.Second * 5)
	go func() {
		for range ticker.C {
			// Before the first full refresh completes, only run the
			// lightweight refresh to avoid heavy work on the hot
			// path. After that, perform the full refresh periodically.
			select {
			case <-server.fullRefreshDone:
				_ = server.Refresh()
			default:
				_ = server.RefreshLight()
			}
		}
	}()

	return server, nil
}

func (s *Server) Refresh() error {
	// Use RefreshLight to populate basic metadata and libraries, then
	// perform the expensive per-library counts and update totals.
	if err := s.RefreshLight(); err != nil {
		return err
	}

	// copy libraries reference under lock to avoid races with concurrent updates
	s.mtx.Lock()
	libs := make([]*Library, len(s.libraries))
	copy(libs, s.libraries)
	s.mtx.Unlock()

	// Ensure each library has a current ItemsCount (this was part of the
	// original full Refresh). We fetch per-library `/all` when cache is
	// absent or expired.
	for _, lib := range libs {
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

		if !usedCache {
			var results ttPlex.SearchResults
			path := "/library/sections/" + lib.ID + "/all"
			s.debugf("Fetching items for library %s (ID: %s, type: %s) from path: %s", lib.Name, lib.ID, lib.Type, path)
			if err := s.Client.Get(path, &results); err == nil {
				lib.ItemsCount = int64(results.MediaContainer.Size)
				if s.LibraryRefreshInterval > 0 {
					// update server cache entry
					s.mtx.Lock()
					for _, l := range s.libraries {
						if l.ID == lib.ID {
							l.lastItemsCount = lib.ItemsCount
							l.lastItemsFetch = time.Now().Unix()
							break
						}
					}
					s.mtx.Unlock()
				}
				s.debugf("Library %s (ID: %s) has %d items", lib.Name, lib.ID, results.MediaContainer.Size)
			} else {
				_ = level.Error(pkgLog).Log("msg", "Error fetching items for library", "library", lib.Name, "id", lib.ID, "err", err)
			}
		}
	}

	moviesTotal, episodesTotal, musicTotal, photosTotal, otherVideosTotal := s.computeLibraryCounts(libs)

	// Update server state under lock with computed totals
	s.mtx.Lock()
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

// RefreshLight performs a minimal refresh that populates server metadata and
// the basic library list (ID, name, type, duration, storage) without
// performing expensive per-library counts like episode or track totals.
func (s *Server) RefreshLight() error {
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

	if err := s.Client.Get("/media/providers?includeStorage=1", &container); err != nil {
		return err
	}

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
				// Try to reuse cached items count if available and fresh
				if s.LibraryRefreshInterval > 0 {
					interval := s.LibraryRefreshInterval
					s.mtx.Lock()
					for _, old := range s.libraries {
						if old.ID == lib.ID {
							if old.lastItemsFetch != 0 && time.Now().Unix()-old.lastItemsFetch < int64(interval.Seconds()) {
								lib.ItemsCount = old.lastItemsCount
								lib.lastItemsCount = old.lastItemsCount
								lib.lastItemsFetch = old.lastItemsFetch
							}
							break
						}
					}
					s.mtx.Unlock()
				}

				newLibraries = append(newLibraries, lib)
			}
		}
	}

	// Update server metadata and libraries atomically.
	s.mtx.Lock()
	s.ID = container.MediaContainer.MachineIdentifier
	s.Name = container.MediaContainer.FriendlyName
	s.Version = container.MediaContainer.Version
	s.libraries = newLibraries
	s.mtx.Unlock()

	// Refresh light-weight server info and resources (best effort)
	_ = s.refreshServerInfo()
	_ = s.refreshResources()

	return nil
}

// computeLibraryCounts performs the expensive per-library counting (episodes,
// music tracks). It mirrors the logic previously embedded in Refresh() and is
// intended to be invoked in background or during full refreshes.
// nolint:gocyclo // function remains complex; consider refactor in a follow-up
func (s *Server) computeLibraryCounts(newLibraries []*Library) (moviesTotal, episodesTotal, musicTotal, photosTotal, otherVideosTotal int64) {
	// Tally straightforward items first
	for _, lib := range newLibraries {
		switch lib.Type {
		case "movie":
			moviesTotal += lib.ItemsCount
		case "music", "artist":
			musicTotal += lib.ItemsCount
		case "photo":
			photosTotal += lib.ItemsCount
		case "homevideo":
			otherVideosTotal += lib.ItemsCount
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	// Reset musicTotal since we'll compute exact track counts
	musicTotal = 0

	for _, lib := range newLibraries {
		if lib.Type == "show" {
			wg.Add(1)
			sem <- struct{}{}
			go func(sectionID string) {
				defer wg.Done()
				defer func() { <-sem }()
				var results ttPlex.SearchResults

				// Check cache
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
					if err := s.Client.Get(path, &results); err == nil {
						mu.Lock()
						episodesTotal += int64(results.MediaContainer.Size)
						mu.Unlock()

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
						_ = level.Error(pkgLog).Log("msg", "Error fetching episodes for section", "section", sectionID, "err", err)
					}
				}
			}(lib.ID)
		}

		if lib.Type == "music" || lib.Type == "artist" {
			wg.Add(1)
			sem <- struct{}{}
			go func(sectionID string) {
				defer wg.Done()
				defer func() { <-sem }()
				var results ttPlex.SearchResults
				var trackCount int64

				interval := s.LibraryRefreshInterval
				useCached := false
				if s.LibraryRefreshInterval > 0 {
					s.mtx.Lock()
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
					for _, trackType := range []string{"10", "7"} {
						path := "/library/sections/" + sectionID + "/all?type=" + trackType
						if err := s.Client.Get(path, &results); err == nil {
							if results.MediaContainer.Size > 0 {
								trackCount = int64(results.MediaContainer.Size)
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
							}
						} else {
							_ = level.Error(pkgLog).Log("msg", "Error fetching music tracks", "type", trackType, "section", sectionID, "err", err)
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

	return
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
