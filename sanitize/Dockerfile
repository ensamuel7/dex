FROM golang:1.27-trixie

RUN apt-get update && apt-get install -y --no-install-recommends \
    clang \
    libclang-rt-19-dev \
    llvm \
    valgrind \
    libcurl4-openssl-dev \
    libhiredis-dev \
    libsqlite3-dev \
    libssl-dev \
    libpq-dev \
    zlib1g-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/clang /usr/local/bin/cc

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
