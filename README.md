# Prometheus Plex Exporter

![Prometheus Plex Exporter](assets/logo.png)

**Expose Plex media server metrics in Prometheus format**

[![CI](https://github.com/timothystewart6/prometheus-plex-exporter/workflows/CI/badge.svg)](https://github.com/timothystewart6/prometheus-plex-exporter/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/timothystewart6/prometheus-plex-exporter)](https://goreportcard.com/report/github.com/timothystewart6/prometheus-plex-exporter)
[![GHCR](https://img.shields.io/badge/ghcr.io-timothystewart6%2Fprometheus--plex--exporter-blue)](https://github.com/timothystewart6/prometheus-plex-exporter/pkgs/container/prometheus-plex-exporter)

---

## Overview

Monitor your Plex Media Server with comprehensive metrics including:

- **Playback Metrics** - Active sessions, user activity, and streaming statistics
- **Storage Metrics** - Library sizes, media counts, and disk usage
- **Server Metrics** - Performance indicators and system health
- **Media Analytics** - Content consumption patterns and transcode statistics

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

### Optional Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_PORT` | `9000` | Port for the metrics endpoint |
| `METRICS_PATH` | `/metrics` | Path for Prometheus metrics |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |

## Metrics

The exporter provides detailed metrics about your Plex server:

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

### Session/Playback Metrics

- `plays_total` - Total play counts with detailed labels (user, device, media, transcode info)
- `play_seconds_total` - Total playback duration per session in seconds
- `estimated_transmit_bytes_total` - Estimated bytes transmitted to clients
- `transmit_bytes_total` - Actual bytes transmitted to clients

### Available Labels

All metrics include relevant labels for detailed filtering and grouping:

- **Server labels**: `server_type`, `server`, `server_id`
- **Library labels**: `library_type`, `library`, `library_id`
- **Session labels**: `media_type`, `title`, `child_title`, `grandchild_title`, `stream_type`, `stream_resolution`, `stream_file_resolution`, `stream_bitrate`, `device`, `device_type`, `user`, `session`, `transcode_type`, `subtitle_action`

## Grafana Dashboard

Import our pre-built Grafana dashboard for instant visualization:

**Dashboard ID:** [Available in examples/dashboards](examples/dashboards/Media%20Server.json)

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

## Acknowledgments

- [jsclayton/prometheus-plex-exporter](hhttps://github.com/jsclayton/prometheus-plex-exporter) - The original repo this was forked from
- [Prometheus](https://prometheus.io) - The monitoring solution
- [Grafana](https://grafana.com) - The visualization platform
- All our [contributors](https://github.com/timothystewart6/prometheus-plex-exporter/graphs/contributors)

---

**If this project helped you, please give it a star!**
