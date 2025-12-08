FROM golang:1.23-alpine3.20 AS build-env

SHELL ["/bin/sh", "-ecuxo", "pipefail"]

RUN set -eux; apk add --no-cache \
    ca-certificates \
    build-base \
    git \
    linux-headers \
    bash \
    binutils-gold \
    wget

WORKDIR /code

# Download go modules + wasmvm static library
ADD go.mod go.sum ./
RUN set -eux; \
    go mod download; \
    \
    # Detect if wasmvm is being used
    WASM_VERSION_LINE=$(go list -m -json all | grep -A2 "github.com/CosmWasm/wasmvm" || true); \
    if [ ! -z "${WASM_VERSION_LINE}" ]; then \
        WASMVM_VERS=$(go list -m all | grep github.com/CosmWasm/wasmvm | awk '{print $2}'); \
        ARCH=$(uname -m); \
        \
        case "$ARCH" in \
            x86_64) ARCH_DL="x86_64" ;; \
            aarch64) ARCH_DL="aarch64" ;; \
            arm64) ARCH_DL="aarch64" ;; \
            *) echo "Unsupported architecture: $ARCH"; exit 1 ;; \
        esac; \
        \
        echo "Downloading wasmvm static library for arch=$ARCH_DL version=$WASMVM_VERS"; \
        wget -O /usr/lib/libwasmvm_muslc.${ARCH_DL}.a \
          https://github.com/CosmWasm/wasmvm/releases/download/${WASMVM_VERS}/libwasmvm_muslc.${ARCH_DL}.a; \
        \
        ln -sf /usr/lib/libwasmvm_muslc.${ARCH_DL}.a /usr/lib/libwasmvm_muslc.a; \
    fi;

# Copy all source
COPY . /code

# Build pchaind as fully static muslc binary
RUN LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make build \
  && file /code/build/pchaind \
  && echo "Ensuring binary is statically linked ..." \
  && (file /code/build/pchaind | grep "statically linked")

# --------------------------------------------------------
FROM alpine:3.21

COPY --from=build-env /code/build/pchaind /usr/bin/pchaind

RUN apk add --no-cache \
    ca-certificates \
    curl \
    make \
    bash \
    jq \
    sed

WORKDIR /opt

EXPOSE 1317 26656 26657 8545 8546

CMD ["/usr/bin/pchaind", "version"]
