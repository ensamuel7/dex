FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends clang libclang-rt-14-dev libcurl4-openssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
