# syntax=docker/dockerfile:1
#
# ytgrabber-server: the app as a self-hosted server. yt-dlp, ffmpeg and
# deno are not baked in: the server installs them into /data on the first
# start (and yt-dlp can be updated from the UI), so the image doesn't go
# stale whenever a site breaks an old yt-dlp.
#
#   docker build -t ytgrabber-server .
#   docker run -p 127.0.0.1:8080:8080 -v ytg-data:/data -v ytg-media:/media ytgrabber-server

ARG GO_VERSION=1.26
ARG NODE_VERSION=24.11.1
# Set by BuildKit; the legacy builder needs --build-arg BUILDPLATFORM=linux/amd64.
ARG BUILDPLATFORM

# --- TypeScript types for the frontend ---
# Wails generates them from the Go structs by compiling the desktop app,
# which needs the GTK/WebKit headers; only this stage has them.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS bindings
RUN apt-get update \
    && apt-get install -y --no-install-recommends libgtk-3-dev libwebkit2gtk-4.1-dev \
    && rm -rf /var/lib/apt/lists/*
ARG WAILS_VERSION=v2.17.1
RUN go install github.com/wailsapp/wails/v2/cmd/wails@${WAILS_VERSION}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# main.go embeds frontend/dist, which doesn't exist yet.
RUN mkdir -p frontend/dist && touch frontend/dist/.keep \
    && wails generate module -tags webkit2_41

# --- Frontend ---
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-bookworm-slim AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
COPY --from=bindings /src/frontend/src/app/wailsjs ./src/app/wailsjs
RUN npm run build

# --- Server ---
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS server
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X ytgrabber/internal/core.Version=${VERSION}" \
    -o /out/ytgrabber-server ./cmd/ytgrabber-server

# --- Runtime ---
# glibc, because the yt-dlp, ffmpeg and deno builds the server installs
# are linked against it.
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --home-dir /data ytgrabber \
    && mkdir -p /data /media \
    && chown ytgrabber:ytgrabber /data /media
COPY --from=server /out/ytgrabber-server /usr/local/bin/ytgrabber-server
COPY --from=web /src/frontend/dist/browser /usr/share/ytgrabber/web

ENV YTG_LISTEN=0.0.0.0:8080 \
    YTG_DATA_DIR=/data \
    YTG_MEDIA_DIR=/media \
    YTG_WEB_DIR=/usr/share/ytgrabber/web \
    HOME=/data
USER ytgrabber
VOLUME ["/data", "/media"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s \
    CMD ["ytgrabber-server", "healthcheck"]
ENTRYPOINT ["ytgrabber-server"]
