# syntax=docker/dockerfile:1.7
# printserver: CUPS + Gutenprint + SANE/AirSane + Avahi + the web UI, under s6.

# Stage 1: GUI. Its output is architecture-independent, so build it natively.
FROM --platform=$BUILDPLATFORM node:24-trixie-slim AS frontend
RUN corepack enable && corepack prepare pnpm@10.32.1 --activate
WORKDIR /app/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm run build

# Stage 2: static Go binary, cross-compiled for the target platform.
FROM --platform=$BUILDPLATFORM golang:1.27-trixie AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/embed.go
COPY --from=frontend /app/web/dist web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /printserver ./cmd/printserver

# Stage 3: AirSane (SANE to eSCL), which Debian does not package. Built for the
# target platform, so the arm64 image builds on an arm64 runner.
FROM debian:trixie-slim AS airsane
ARG AIRSANE_VERSION=v0.4.9
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates git cmake make g++ \
      libsane-dev libjpeg-dev libpng-dev libavahi-client-dev libusb-1.0-0-dev \
    && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch "$AIRSANE_VERSION" https://github.com/SimulPiscator/AirSane.git /src \
    && cmake -S /src -B /build -DCMAKE_BUILD_TYPE=Release \
    && cmake --build /build -j"$(nproc)" \
    && install -D -m 0755 /build/airsaned /out/usr/local/bin/airsaned \
    && install -d /out/etc/airsane \
    && install -m 0644 /src/etc/ignore.conf /src/etc/access.conf /out/etc/airsane/ \
    && install -m 0644 /src/etc/Gnome-scanner.png /out/etc/airsane/ \
    && printf 'icon Gnome-scanner.png\n' >/out/etc/airsane/options.conf

# Stage 4: runtime.
FROM debian:trixie-slim
ARG TARGETARCH
ARG VERSION=dev
LABEL org.opencontainers.image.source="https://github.com/jasonthagerty/printserver" \
      org.opencontainers.image.description="CUPS, AirPrint, AirScan and a mobile web UI for a USB printer, sized for a MikroTik router" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.version="${VERSION}"
ARG S6_OVERLAY_VERSION=3.2.3.2
RUN apt-get update && apt-get install -y --no-install-recommends \
      cups cups-filters printer-driver-gutenprint ghostscript poppler-utils \
      avahi-daemon dbus libnss-mdns \
      libsane1 sane-utils libjpeg62-turbo libpng16-16t64 libavahi-client3 libusb-1.0-0 \
      ca-certificates curl xz-utils \
    && case "$TARGETARCH" in amd64) s6arch=x86_64 ;; arm64) s6arch=aarch64 ;; *) echo "unsupported $TARGETARCH" >&2; exit 1 ;; esac \
    && base="https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}" \
    && cd /tmp \
    && for t in noarch "$s6arch"; do \
         curl -fsSLO "$base/s6-overlay-$t.tar.xz" && curl -fsSLO "$base/s6-overlay-$t.tar.xz.sha256" \
         && sha256sum -c "s6-overlay-$t.tar.xz.sha256" && tar -C / -Jxpf "s6-overlay-$t.tar.xz"; \
       done \
    && rm -f /tmp/s6-overlay-* \
    && apt-get purge -y --auto-remove curl xz-utils \
    && rm -rf /var/lib/apt/lists/* /etc/cups/printers.conf /etc/cups/ppd
COPY --from=airsane /out/ /
COPY --from=build /printserver /usr/local/bin/printserver
COPY image/rootfs/ /
RUN ldd /usr/local/bin/airsaned | { ! grep 'not found'; }

ENV S6_KEEP_ENV=1 \
    S6_BEHAVIOUR_IF_STAGE2_FAILS=2 \
    S6_CMD_WAIT_FOR_SERVICES_MAXTIME=0 \
    S6_VERBOSITY=1 \
    PRINTSERVER_DEVICE=test \
    PRINTSERVER_HOSTNAME=printserver \
    PRINTSERVER_DATA=/data \
    PRINTSERVER_LISTEN=:80 \
    PRINTSERVER_CUPS_SOCKET=/run/cups/cups.sock \
    PRINTSERVER_ESCL_URL=http://127.0.0.1:18090/eSCL \
    PRINTSERVER_ESCL_LISTEN=:8090 \
    PRINTSERVER_ANNOUNCE_PRINTER=1

EXPOSE 80 631 8090
# Docker uses this; RouterOS takes the same command as healthcheck-cmd
# (docs/runbooks/deploy-ap2.md).
HEALTHCHECK --interval=1m --timeout=10s --start-period=2m --retries=3 \
  CMD ["/usr/local/bin/printserver", "healthcheck"]
ENTRYPOINT ["/init"]
