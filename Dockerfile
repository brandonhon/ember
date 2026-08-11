# syntax=docker/dockerfile:1.7
#
# Base images are pinned by digest so a rebuild of an old commit produces the
# same image it did originally, and so the toolchain that compiled a release is
# recorded here rather than inferred from the build date. Each digest is the
# multi-arch index, not a per-platform manifest — the release build is
# linux/amd64 + linux/arm64, and a platform-specific digest would break it. The
# tag is kept alongside for readability; Docker resolves the digest and ignores
# it. Dependabot moves both together (.github/dependabot.yml).

# Build the Svelte SPA.
FROM node:20-alpine@sha256:fb4cd12c85ee03686f6af5362a0b0d56d50c58a04632e6c0fb8363f609372293 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Build the Go binary (CGO_ENABLED=0 — modernc.org/sqlite is pure Go).
# The image's Go version must satisfy go.mod's `go` directive, and the digest
# freezes it at an exact patch (1.26.5 as pinned, which is what go.mod asks
# for). Raising the `go` directive past the pinned toolchain therefore breaks
# this build until the digest moves too — bump them in the same change.
FROM golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Replace the embed placeholder with the freshly-built SPA.
RUN rm -rf internal/web/dist && mkdir -p internal/web/dist
COPY --from=web /src/web/dist/ internal/web/dist/
ARG VERSION=docker
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/ember ./cmd/ember

# Stage a /data directory owned by the distroless nonroot UID (65532). When a
# named volume is mounted at /data, Docker initializes its permissions from
# this pre-existing directory — without this, the volume is root-owned and
# the nonroot user can't write ember.db.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

# Final image: distroless (no shell, no package manager).
FROM gcr.io/distroless/static-debian12:nonroot@sha256:1b7b9f0f0e0a1d2155f531db587cc48ec26aaf97ab64364225f5bf18a054e66a AS final
COPY --from=build /out/ember /ember
COPY --from=build --chown=nonroot:nonroot /out/data /data
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["/ember", "version"]
ENTRYPOINT ["/ember"]
