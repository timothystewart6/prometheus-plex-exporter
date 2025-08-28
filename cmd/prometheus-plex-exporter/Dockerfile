# Intermediate alpine stage to install tzdata and CA certs. Some minimal
# images don't include zoneinfo files; installing tzdata here ensures the
# files exist so we can copy them into the final scratch image.
FROM alpine:3.22.1 AS tz
RUN apk add --no-cache tzdata ca-certificates

# Ultra-lean production image - only CA certs and zoneinfo needed for HTTPS and TZ
FROM scratch
ARG TARGETARCH

# Copy CA certificates for HTTPS support
COPY --from=tz /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy full zoneinfo directory and a sane /etc/localtime (UTC) fallback
COPY --from=tz /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=tz /usr/share/zoneinfo/UTC /etc/localtime

# Copy the optimized binary
COPY bin/linux/prometheus-plex-exporter-${TARGETARCH} /prometheus-plex-exporter

# Run as non-root user (nobody)
USER 65534:65534

ENTRYPOINT ["/prometheus-plex-exporter"]