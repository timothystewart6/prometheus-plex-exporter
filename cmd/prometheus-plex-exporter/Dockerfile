# Ultra-lean production image - only CA certs needed for HTTPS Plex servers
FROM scratch
ARG TARGETARCH

# Copy CA certificates for HTTPS support (no timezone data needed)
COPY --from=alpine:3.22.1 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Include tzdata (zoneinfo) so IANA timezone names work inside the container.
# This makes time.LoadLocation("America/Chicago") succeed without embedding tzdata
# into the binary. We copy the zoneinfo directory and /etc/localtime from
# the Alpine image into the scratch image.
COPY --from=alpine:3.22.1 /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=alpine:3.22.1 /etc/localtime /etc/localtime

# Copy the optimized binary
COPY bin/linux/prometheus-plex-exporter-${TARGETARCH} /prometheus-plex-exporter

# Run as non-root user (nobody)
USER 65534:65534

ENTRYPOINT ["/prometheus-plex-exporter"]