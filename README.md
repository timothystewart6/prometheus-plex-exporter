# Prometheus Plex Exporter

<div align="center">
  <img src="assets/logo.png" alt="Prometheus Plex Exporter" width="250">
  
  **Expose Plex media server metrics in Prometheus format**
  
  [![CI](https://github.com/timothystewart6/prometheus-plex-exporter/workflows/CI/badge.svg)](https://github.com/timothystewart6/prometheus-plex-exporter/actions)
  [![Go Report Card](https://goreportcard.com/badge/github.com/timothystewart6/prometheus-plex-exporter)](https://goreportcard.com/report/github.com/timothystewart6/prometheus-plex-exporter)
  [![GHCR](https://img.shields.io/badge/ghcr.io-timothystewart6%2Fprometheus--plex--exporter-blue)](https://github.com/timothystewart6/prometheus-plex-exporter/pkgs/container/prometheus-plex-exporter)
</div>

---

## Overview

Monitor your Plex Media Server with comprehensive real-time metrics including:

- **Advanced Transcode Analytics** - Detailed transcode type detection (video/audio/both) and subtitle handling
- **Network & Bandwidth** - Real-time transmission tracking and bandwidth monitoring
- **Real-time Session Monitoring** - Live WebSocket tracking of playback sessions and state changes
- **Server Performance** - CPU/memory utilization and system health indicators
- **Storage & Library Metrics** - Library sizes, media counts, and disk usage across all sections

## Quick Start

### Docker (Recommended)

```bash
docker run -d \
  --name plex-exporter \
  -p 9000:9000 \
  -e PLEX_SERVER="http://192.168.1.100:32400" \
  -e PLEX_TOKEN="your-plex-token-here" \
  ghcr.io/timothystewart6/prometheus-plex-exporter
```

### Docker Compose

```yaml
version: '3.8'
services:
  plex-exporter:
    image: ghcr.io/timothystewart6/prometheus-plex-exporter
    container_name: plex-exporter
    ports:
      - "9000:9000"
    environment:
      PLEX_SERVER: "http://192.168.1.100:32400"
      PLEX_TOKEN: "your-plex-token-here"
    restart: unless-stopped
```

## Configuration

## Metrics

### Environment Variables

You can supply environment variables via your container runtime, `docker run -e VAR=...`, `docker-compose`, or with a `.env` file. The exporter recognizes the following environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `PLEX_SERVER` | Full URL to your Plex server (required) | `(required)` |
| `PLEX_TOKEN` | [Plex authentication token](https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/) used to authenticate with your Plex server (required) | `(required)` |
| `LIBRARY_REFRESH_INTERVAL` | How often to re-query expensive per-library counts (music tracks, episode counts, and library items) in minutes. Must be an integer number of minutes. Defaults to `15` when unset. Set to `0` to disable caching and always re-query. | `15` |
| `DEBUG` | Enable verbose debug logging. Set to `true` to turn on debug-level logs (useful for troubleshooting). Defaults to off. | `false` |
| `SKIP_TLS_VERIFICATION` | Optional convenience for connecting to Plex servers with self-signed or mismatched TLS certificates. When set to `true` the exporter (and the vendored Plex client) will skip TLS certificate verification for both HTTP and websocket connections. THIS IS INSECURE — only use in trusted networks or testing. Defaults to off. | `false` |
| `TZ` | Optional timezone name (IANA location, e.g. `America/Chicago`) to use for log timestamps and any local time calculations. If unset the system local timezone is used. | `(system)` |

Example `.env` file:

```bash
PLEX_SERVER=http://192.168.1.100:32400
PLEX_TOKEN=abcd1234efgh5678
TZ=America/Chicago
```

Startup note:

- On startup the exporter performs a fast, lightweight metadata refresh to discover server and library sections and reduce startup latency and memory use. The expensive per-library full refresh (which computes episode counts, exact music track counts, and full library item lists) is deferred and runs in the background after a short delay. The startup log will always show the effective `LibraryRefreshInterval` or that caching is disabled.
- The required `PLEX_SERVER` and `PLEX_TOKEN` environment variables are required for the exporter to connect to your Plex server. Note: unit tests in this repository perform a synchronous full refresh so counts are populated deterministically during tests; production behavior defers the heavy work.

Library refresh timing (production)

- The exporter defers the expensive per-library full refresh for 15 seconds after startup (plus a small jitter) and performs a fast, lightweight discovery immediately. This reduces peak memory and server load during startup. The 15s startup delay is a fixed behavior in the binary (not exposed as an environment variable).

Recommendation: Keep the default deferred behavior (15s delay) in production to avoid simultaneous heavy queries and lower peak memory usage. If you're running many exporter instances against one Plex server, consider staggering startups.

The exporter provides comprehensive real-time metrics about your Plex server via WebSocket monitoring:

### Server Metrics

- `host_cpu_util` - Host CPU utilization percentage
- `host_mem_util` - Host memory utilization percentage
- `server_info` - Server information with version, platform details

### Library Metrics

- `library_duration_total` - Total duration of library content in milliseconds
- `library_storage_total` - Total storage size of library in bytes

### Media Count Metrics

- `plex_library_items` - Number of items in each library section (movies for movie libraries, shows for TV libraries, artists for music libraries, photos for photo libraries). Includes a `content_type` label to clarify what type of items are being counted.
- `plex_media_episodes` - Total number of TV episodes across all libraries
- `plex_media_music` - Total number of music tracks across all libraries
- `plex_media_movies` - Total number of movies across all libraries
- `plex_media_other_videos` - Total number of other videos (home videos) across all libraries
- `plex_media_photos` - Total number of photos across all libraries

### Real-time Session/Playback Metrics

- `estimated_transmit_bytes_total` - Estimated bytes transmitted to clients
- `play_seconds_total` - Total playback duration per session in seconds
- `plays_total` - Total play counts with detailed labels (user, device, media, transcode info)
- `transmit_bytes_total` - Actual bytes transmitted to clients

### Advanced Transcode Metrics

All session metrics include sophisticated transcode detection:

- **Stream Analysis**: Source vs destination resolution, bitrate, and codec comparison
- **Subtitle Actions**: `burn`, `copy`, `none`, or `transcode` for subtitle handling
- **Transcode Type**: `video`, `audio`, `both`, or `none` based on codec analysis

### Available Labels

All metrics include relevant labels for detailed filtering and grouping:

- **Library labels**: `library_type`, `library`, `library_id`
- **Server labels**: `server_type`, `server`, `server_id`
- **Session labels**: `media_type`, `title`, `child_title`, `grandchild_title`, `stream_type`, `stream_resolution`, `stream_file_resolution`, `stream_bitrate`, `device`, `device_type`, `user`, `session`, `transcode_type`, `subtitle_action`

## Grafana Dashboard

Import our pre-built Grafana dashboard for instant visualization:

**Dashboard ID:** [Available in examples/dashboards](examples/dashboards/Media%20Server.json)

## Real-time Monitoring Features

This exporter provides advanced real-time monitoring capabilities:

### WebSocket Integration

- **Live Session Tracking**: Connects to Plex's WebSocket API for instant session updates
- **State Change Detection**: Real-time monitoring of play/pause/stop/buffering states  
- **Session Lifecycle Management**: Automatic session pruning and cardinality control

### Performance Optimizations

- **Concurrent Library Scanning**: Parallel processing of library sections with semaphore control
- **Exponential Backoff**: Smart retry logic for session and metadata lookups
- **Efficient Memory Usage**: Automatic cleanup of old session metrics to prevent memory bloat

### Session Intelligence

- **Smart Transcode Detection**: Analyzes codec changes and Plex decisions for accurate classification
- **Bandwidth Estimation**: Real-time tracking of transmitted data and network usage
- **Session Correlation**: Maps transcode sessions to playback sessions for detailed analytics

## Development

### Building from Source

```bash
# Clone the repository
git clone https://github.com/timothystewart6/prometheus-plex-exporter.git
cd prometheus-plex-exporter

# Build the binary
go build -o prometheus-plex-exporter ./cmd/prometheus-plex-exporter

# Run locally
./prometheus-plex-exporter
```

### Customization

Currently, the following values are hardcoded but can be modified in the source:

- **Listen Port**: Hardcoded to `:9000` in `cmd/prometheus-plex-exporter/main.go`
- **Metrics Path**: Hardcoded to `/metrics` in `cmd/prometheus-plex-exporter/main.go`  
- **Log Level**: Uses `logfmt` format without configurable levels

To customize these values, modify the constants in `main.go` and rebuild.

### Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run integration tests
go test ./pkg/plex -run TestIntegration
```

## Contributing

We welcome contributions! For developer setup, pre-commit checks, linter installation, and the full local workflow please see `CONTRIBUTING.md` in the repository root.

You can skip the hook for a single commit with `git commit --no-verify`, but avoid doing that for PRs unless there is a valid reason.

### Installing the recommended linter (golangci-lint)

We recommend installing `golangci-lint` for fast, consistent linting. It runs many linters in parallel and respects the project's `.golangci.yml`.

macOS (Homebrew):

```bash
brew install golangci-lint
```

Linux (bash):

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.59.0
# ensure $(go env GOPATH)/bin is in your PATH
```

Or see the official instructions: <https://golangci-lint.run/usage/install/>

### Running checks locally

Format code:

```bash
gofmt -s -w $(git ls-files '*.go')
```

Lint (preferred):

```bash
golangci-lint run ./...
```

Fallback lint (if you don't have golangci-lint):

```bash
go vet ./...
```

Run tests:

```bash
go test ./...
```

### Branching and PRs

1. Fork the repo and create a feature branch from `main`.
2. Make changes and ensure pre-commit checks pass locally.
3. Open a PR against `main` with a clear description of the change.

CI will run the linter and tests on your PR. Please address any lint/test failures before merging.

Thanks — contributions are welcome!

## Container Images & Releases

### Rolling Release Strategy

This project follows a **rolling release model** where the `main` branch is automatically built and deployed to GitHub Container Registry (GHCR). Every commit to `main` produces a new `:latest` image, ensuring you always have access to the newest features and bug fixes.

### Stable Deployments

For production environments requiring stability, we recommend **pinning to a specific digest** rather than using the `:latest` tag. This ensures your deployment won't change unexpectedly.

#### Example: Pin to Digest

```bash
# Get the current digest
docker pull ghcr.io/timothystewart6/prometheus-plex-exporter:latest
docker inspect ghcr.io/timothystewart6/prometheus-plex-exporter:latest | grep -A 1 "RepoDigests"

# Use the digest in your deployment
docker run -d \
  --name plex-exporter \
  -p 9000:9000 \
  -e PLEX_SERVER="http://192.168.1.100:32400" \
  -e PLEX_TOKEN="your-plex-token-here" \
  ghcr.io/timothystewart6/prometheus-plex-exporter@sha256:abc123...
```

#### Docker Compose with Digest

```yaml
version: '3.8'
services:
  plex-exporter:
    image: ghcr.io/timothystewart6/prometheus-plex-exporter@sha256:abc123def456...
    container_name: plex-exporter
    ports:
      - "9000:9000"
    environment:
      PLEX_SERVER: "http://192.168.1.100:32400"
      PLEX_TOKEN: "your-plex-token-here"
    restart: unless-stopped
```

> **💡 Tip**: You can find specific digests in the [GHCR packages page](https://github.com/timothystewart6/prometheus-plex-exporter/pkgs/container/prometheus-plex-exporter) or by using `docker inspect` as shown above.

## Acknowledgments

- [jsclayton/prometheus-plex-exporter](https://github.com/jsclayton/prometheus-plex-exporter) - The original repo this was forked from
- [Prometheus](https://prometheus.io) - The monitoring solution
- [Grafana](https://grafana.com) - The visualization platform
- All our [contributors](https://github.com/timothystewart6/prometheus-plex-exporter/graphs/contributors)

---

**If this project helped you, please give it a star!**
