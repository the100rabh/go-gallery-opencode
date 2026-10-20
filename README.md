# Media Gallery

A small, lightweight, self-hosted media gallery for browsing images and
videos stored on a server — built primarily for a **Raspberry Pi running
Docker on a trusted home LAN**.

> **Do one thing well:** browse locally stored photos and videos through a
> simple web interface with minimal CPU, memory, storage and operational
> complexity.

- Single self-contained Go binary; the frontend (plain HTML/CSS/vanilla JS)
  is embedded in the binary. No npm, no Node.js, no frontend build step.
- No database, no media index, no thumbnails, no transcoding, no FFmpeg,
  no background workers. The requested directory is scanned fresh on every
  request, so added/removed files appear immediately.
- The media directory is only ever **read**; the Docker mount is read-only.
- Only the requested directory is scanned — never the whole media tree.

## Installation (Docker Compose — recommended)

```bash
cp config.example.yaml config.yaml   # then edit, especially the password
nano docker-compose.yml              # point /path/to/photos at your media
docker compose up -d
```

Then open `http://raspberrypi:8080`.

`docker-compose.yml` mounts your photos read-only and the config read-only:

```yaml
volumes:
  - /path/to/photos:/media:ro
  - ./config.yaml:/etc/media-gallery/config.yaml:ro
```

The image is a multi-stage build: `golang:1.24-alpine` compiles a static
binary (`CGO_ENABLED=0`), which is copied into a
`gcr.io/distroless/static-debian12:nonroot` runtime image. It builds and
runs on `linux/arm64` (Raspberry Pi 3/4/5 with a 64-bit OS) and `linux/amd64`.
To build/push a multi-architecture image:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t yourname/media-gallery --push .
```

## Configuration

YAML, supplied with `-config` (the compose file already does this):

```yaml
server:
  host: "0.0.0.0"     # "" or "0.0.0.0" = all interfaces
  port: 8080

media:
  root: "/media"      # directory containing your media
  page_size: 50       # files per page (1-500)

auth:
  enabled: true
  username: "admin"
  password: "change-me"
```

Notes:

- The config file is optional; without it the defaults above apply.
- Unknown keys are rejected at startup (typos fail loudly).
- Startup fails clearly on: invalid YAML, missing/not-a-directory media
  root, invalid port, invalid page size, and auth enabled without
  credentials.
- Environment variable overrides are not implemented in v1.

## Running without Docker

```bash
make build
./media-gallery -config /etc/media-gallery/config.yaml
```

Flags: `-config <path>` (optional), `-log-level debug|info|warn|error`,
`-version`.

## Supported media

Only formats modern browsers can display natively; everything else is
ignored (never converted):

| Type  | Extensions              |
|-------|-------------------------|
| Image | `.jpg` `.jpeg` `.png` `.gif` `.webp` `.avif` |
| Video | `.mp4` `.m4v` `.webm`   |

## Using the gallery

- **Navigation:** folders appear first in the grid; click to enter. The URL
  reflects the location (`/gallery/2025/Trip/`), breadcrumbs (`Home / 2025 /
  Trip`) take you back, and refresh/back/forward work.
- **Sorting:** by name, date (filesystem modification time) or size, in
  either direction. Default: date descending.
- **Pagination:** files are paginated (`page_size`); **directories are
  always shown in full** and are never paginated away. Requesting a page
  beyond the last one returns an empty file list.
- **Opening media:** clicking an image or video opens the original file in
  a new browser tab (`/media/2025/Trip/IMG_1234.jpg`). Videos stream with
  HTTP range requests, so seeking works. There is no lightbox by design.
- **Previews:** the browser renders previews with native `<img>` /
  `<video preload="metadata">` elements. No server-side thumbnails are
  generated, aspect ratios are preserved, and if the browser cannot decode
  a file the card shows a fallback message while the file stays accessible.

## API

One read-only endpoint:

```
GET /api/browse?path=/2025/Trip&page=1&page_size=50&sort=date&order=desc
```

Returns the path, breadcrumbs, directories (unpaginated), one page of
supported media files, and pagination metadata. Invalid parameters get
`400`; invalid paths get `403`; missing directories get `404`.

## Security

- **Path handling:** user-supplied paths are strictly validated (`..`,
  `.`/empty segments, backslashes, NUL/control characters rejected) and
  resolved against the media root with symlink resolution checked, so
  nothing outside the configured root can be read or listed. Only supported
  media files are ever served — configuration, Docker files and the binary
  are not exposed.
- **Authentication:** HTTP Basic auth (constant-time comparison) protects
  the UI, API, listings and media files.
- **Threat model:** this app is designed for a **trusted home LAN**. HTTP
  Basic authentication sends base64-encoded (not encrypted) credentials and
  provides no transport security. If you ever expose it beyond your LAN,
  put it behind an HTTPS reverse proxy (e.g. Caddy, nginx + TLS) and enable
  authentication there as well.

## Design decisions

- **Rescan per request:** directory contents are read, filtered, sorted and
  paginated on every browse request. No watcher, no cache invalidation
  problem; changes on disk are visible on the next request.
- **No dimensions in metadata (v1):** image/video dimensions would require
  parsing or decoding file headers on every listing; filename, size,
  modification time and media type are cheap and sufficient. The browser
  preserves aspect ratios naturally.
- **Symlinks:** skipped in listings; a symlink resolving outside the media
  root is refused when accessed directly.
- **Hidden files:** entries starting with `.` are hidden from listings and
  cannot be opened.
- **`mtime` for date sorting:** no EXIF parsing.
- **Logging:** logfmt-style lines on stdout, e.g.
  `2026-09-25T21:30:15+05:30 INFO server started address=:8080`.
  Successful media requests are logged only at debug level; failures are
  logged with details that are never sent to clients.
- **Graceful shutdown:** SIGINT/SIGTERM drain in-flight requests (10s).
- **Dependencies:** Go standard library plus `gopkg.in/yaml.v3` (YAML
  parsing is impractical with the standard library). Nothing else.

## Development

```bash
make fmt      # go fmt
make vet      # go vet
make test     # go test ./...
make build    # static binary -> ./media-gallery
make run      # build + run with ./config.yaml
make docker   # docker build
```

Tests cover path security (including traversal and symlinks), directory
browsing, sorting, pagination, media serving with range requests,
authentication and configuration validation.

## Explicit non-goals (v1)

No uploads, deletes, renames, edits, albums, tags, search, EXIF handling,
face recognition, duplicates detection, slideshows, lightboxes, sharing
links, user accounts, OAuth, JWT, HTTPS, databases, background indexing,
file watchers, WebSockets, or any frontend framework. The goal is a small,
understandable, resource-efficient tool.
