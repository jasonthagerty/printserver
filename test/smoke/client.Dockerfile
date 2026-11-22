# A LAN client for the smoke test: mDNS browsing, IPP, curl.
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      avahi-daemon avahi-utils dbus libnss-mdns cups-client cups-ipp-utils curl ca-certificates poppler-utils jq \
    && rm -rf /var/lib/apt/lists/*
COPY test/smoke/client-entry.sh /usr/local/bin/client-entry
ENTRYPOINT ["/usr/local/bin/client-entry"]
