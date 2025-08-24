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

- **Real-time Session Monitoring** - Live WebSocket tracking of playback sessions and state changes
- **Advanced Transcode Analytics** - Detailed transcode type detection (video/audio/both) and subtitle handling
- **Storage & Library Metrics** - Library sizes, media counts, and disk usage across all sections
- **Server Performance** - CPU/memory utilization and system health indicators
- **Network & Bandwidth** - Real-time transmission tracking and bandwidth monitoring

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

### Required Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `PLEX_SERVER` | Full URL to your Plex server | `http://192.168.1.100:32400` |
| `PLEX_TOKEN` | [Plex authentication token](https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/) | `abcd1234efgh5678` |

## Metrics

The exporter provides comprehensive real-time metrics about your Plex server via WebSocket monitoring:

### Server Metrics

- `server_info` - Server information with version, platform details
- `host_cpu_util` - Host CPU utilization percentage  
- `host_mem_util` - Host memory utilization percentage

### Library Metrics

- `library_duration_total` - Total duration of library content in milliseconds
- `library_storage_total` - Total storage size of library in bytes
- `plex_library_items` - Number of items in each library section

### Media Count Metrics

- `plex_media_movies` - Total number of movies across all libraries
- `plex_media_episodes` - Total number of TV episodes across all libraries
- `plex_media_music` - Total number of music tracks across all libraries
- `plex_media_photos` - Total number of photos across all libraries
- `plex_media_other_videos` - Total number of other videos (home videos) across all libraries

### Real-time Session/Playback Metrics

- `plays_total` - Total play counts with detailed labels (user, device, media, transcode info)
- `play_seconds_total` - Total playback duration per session in seconds
- `estimated_transmit_bytes_total` - Estimated bytes transmitted to clients
- `transmit_bytes_total` - Actual bytes transmitted to clients

### Advanced Transcode Metrics

All session metrics include sophisticated transcode detection:

- **Transcode Type**: `video`, `audio`, `both`, or `none` based on codec analysis
- **Subtitle Actions**: `burn`, `copy`, or `none` for subtitle handling
- **Stream Analysis**: Source vs destination resolution, bitrate, and codec comparison

### Available Labels

All metrics include relevant labels for detailed filtering and grouping:

- **Server labels**: `server_type`, `server`, `server_id`
- **Library labels**: `library_type`, `library`, `library_id`
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

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

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
