# syntax=docker/dockerfile:1
#
# Multi-stage build producing a tiny static image that runs on Raspberry Pi
# (linux/arm64) and amd64. No Node.js, no frontend build step.

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/media-gallery ./cmd/media-gallery

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/media-gallery /media-gallery
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/media-gallery"]
