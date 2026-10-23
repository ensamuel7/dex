# Stage 1: Build the dex compiler
#
# The Go version is a security property of the image, not only a build detail:
# the dex binary carries the standard library it was built with, and a scanner
# reports that library's CVEs against the image. Keep this on a supported Go
# release (the latest two minor versions), and in step with release.yml.
FROM golang:1.27-trixie AS builder

ARG DEX_VERSION=dev

WORKDIR /src

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN go build -ldflags "-s -w -X main.Version=${DEX_VERSION}" -o /usr/local/bin/dex

# Stage 2: Runtime image with C toolchain
#
# Debian 13 by name rather than "stable", so a Debian release cannot change the
# toolchain under a build that worked. The upgrade picks up the security fixes
# published since the base image was cut, which is most of what a scan finds.
FROM debian:trixie-slim

RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    ca-certificates \
    gcc \
    git \
    libc6-dev \
    libcurl4-openssl-dev \
    libhiredis-dev \
    libsqlite3-dev \
    libssl-dev \
    libpq-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/dex /usr/local/bin/dex

WORKDIR /workspace

ENTRYPOINT ["dex"]
CMD ["version"]
