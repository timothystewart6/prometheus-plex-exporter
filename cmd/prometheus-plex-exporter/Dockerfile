# Ultra-lean production image - only CA certs needed for HTTPS Plex servers
FROM scratch
ARG TARGETARCH

# Copy CA certificates for HTTPS support (no timezone data needed)
COPY --from=alpine:3.22.1 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the optimized binary
COPY bin/linux/prometheus-plex-exporter-${TARGETARCH} /prometheus-plex-exporter

# Run as non-root user (nobody)
USER 65534:65534

ENTRYPOINT ["/prometheus-plex-exporter"]