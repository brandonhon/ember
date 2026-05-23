# syntax=docker/dockerfile:1.7

# Build the Svelte SPA.
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Build the Go binary (CGO_ENABLED=0 — modernc.org/sqlite is pure Go).
# The image's Go version must satisfy go.mod's `go` directive.
FROM golang:1.26-alpine AS build
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
FROM gcr.io/distroless/static-debian12:nonroot AS final
COPY --from=build /out/ember /ember
COPY --from=build --chown=nonroot:nonroot /out/data /data
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["/ember", "version"]
ENTRYPOINT ["/ember"]
