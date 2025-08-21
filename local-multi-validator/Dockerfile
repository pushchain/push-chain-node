FROM golang:1.23.7-alpine3.20 AS build-env

SHELL ["/bin/sh", "-ecuxo", "pipefail"]

RUN set -eux; apk add --no-cache \
    ca-certificates \
    build-base \
    git \
    linux-headers \
    bash \
    binutils-gold

WORKDIR /code

ADD go.mod go.sum ./
RUN set -eux; \
    export ARCH=$(uname -m); \
    WASM_VERSION=$(go list -m all | grep github.com/CosmWasm/wasmvm || true); \
    if [ ! -z "${WASM_VERSION}" ]; then \
      WASMVM_REPO=$(echo $WASM_VERSION | awk '{print $1}');\
      WASMVM_VERS=$(echo $WASM_VERSION | awk '{print $2}');\
      wget -O /lib/libwasmvm_muslc.a https://${WASMVM_REPO}/releases/download/${WASMVM_VERS}/libwasmvm_muslc.$(uname -m).a;\
    fi; \
    go mod download;

# Copy over code
COPY . /code

# Build both binaries
RUN LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make build \
  && file /code/build/pchaind \
  && file /code/build/puniversald \
  && echo "Ensuring binaries are statically linked ..." \
  && (file /code/build/pchaind | grep "statically linked") \
  && (file /code/build/puniversald | grep "statically linked")

# --------------------------------------------------------
FROM alpine:3.21

# Copy both binaries
COPY --from=build-env /code/build/pchaind /usr/bin/pchaind
COPY --from=build-env /code/build/puniversald /usr/bin/puniversald

RUN apk add --no-cache ca-certificates curl make bash jq sed

WORKDIR /opt

# Copy setup scripts
COPY local-multi-validator/scripts/ /opt/scripts/

# Expose ports for core validators (RPC, GRPC, P2P) and universal validators (Query API)
EXPOSE 1317 26656 26657 26658 26659 8545 8546 9090 9091 9092 8080 8081 8082

CMD ["/usr/bin/pchaind", "version"]