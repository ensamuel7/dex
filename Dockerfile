# Stage 1: Build the dex compiler
FROM golang:1.25-bookworm AS builder

ARG DEX_VERSION=dev

WORKDIR /src

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN go build -ldflags "-s -w -X main.Version=${DEX_VERSION}" -o /usr/local/bin/dex

# Stage 2: Runtime image with C toolchain
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    libcurl4-openssl-dev \
    libsqlite3-dev \
    libssl-dev \
    libpq-dev \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/dex /usr/local/bin/dex

WORKDIR /workspace

ENTRYPOINT ["dex"]
CMD ["version"]
