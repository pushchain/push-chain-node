FROM golang:1.23.7-alpine3.20 AS build-env

SHELL ["/bin/sh", "-ecuxo", "pipefail"]

# Build arg for CI token (optional, for private repos)
ARG CI_DKLS_GARBLING
ENV CI_DKLS_GARBLING=${CI_DKLS_GARBLING}

RUN set -eux; apk add --no-cache \
    ca-certificates \
    build-base \
    git \
    linux-headers \
    bash \
    binutils-gold \
    curl

# Install Rust for building dkls23-rs
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

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

# Setup dkls23-rs: configure Git and clone repository
# Clone to /dkls23-rs (parent of /code) so Makefile can find it at ../dkls23-rs
RUN set -eux; \
    if [ ! -z "${CI_DKLS_GARBLING}" ]; then \
      git config --global url."https://${CI_DKLS_GARBLING}@github.com/pushchain/".insteadOf "https://github.com/pushchain/"; \
      git config --global url."https://github.com/".insteadOf "git@github.com:"; \
      git config --global credential.helper store; \
      echo "https://${CI_DKLS_GARBLING}@github.com" > ~/.git-credentials; \
      chmod 600 ~/.git-credentials; \
      mkdir -p ~/.cargo; \
      echo '[net]' >> ~/.cargo/config.toml; \
      echo 'git-fetch-with-cli = true' >> ~/.cargo/config.toml; \
      if [ ! -d "/dkls23-rs" ]; then \
        git clone --depth 1 https://${CI_DKLS_GARBLING}@github.com/pushchain/dkls23-rs.git /dkls23-rs; \
      fi; \
    else \
      echo "Warning: CI_DKLS_GARBLING not set, skipping dkls23-rs clone. Build may fail if dkls23-rs is required."; \
    fi

# force it to use static lib (from above) not standard libgo_cosmwasm.so file
# then log output of file /code/bin/pchaind
# then ensure static linking
RUN LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make build \
  && file /code/build/pchaind \
  && echo "Ensuring binary is statically linked ..." \
  && (file /code/build/pchaind | grep "statically linked")

# --------------------------------------------------------
FROM alpine:3.21

COPY --from=build-env /code/build/pchaind /usr/bin/pchaind

RUN apk add --no-cache ca-certificates curl make bash jq sed

WORKDIR /opt

# rest server, tendermint p2p, tendermint rpc
EXPOSE 1317 26656 26657 8545 8546

CMD ["/usr/bin/pchaind", "version"]
