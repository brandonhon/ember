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
FROM node:26-alpine@sha256:ef24c5053d50fdc3e4e56eb4e7ddb7861874ab0fdc797046ba897581deb8e868 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Build the Go binary (CGO_ENABLED=0 — modernc.org/sqlite is pure Go).
# The image's Go version must satisfy go.mod's `go` directive, and the digest
# freezes it at an exact patch (1.26.7 as pinned, which is what go.mod asks
# for). Raising the `go` directive past the pinned toolchain therefore breaks
# this build until the digest moves too — bump them in the same change.
FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
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
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS final
COPY --from=build /out/ember /ember
COPY --from=build --chown=nonroot:nonroot /out/data /data
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["/ember", "version"]
ENTRYPOINT ["/ember"]
