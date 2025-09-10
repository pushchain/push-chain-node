#!/usr/bin/make -f

PACKAGES_SIMTEST=$(shell go list ./... | grep '/simulation')
VERSION := $(shell echo $(shell git describe --tags) | sed 's/^v//')
COMMIT := $(shell git log -1 --format='%H')
LEDGER_ENABLED ?= true
SDK_PACK := $(shell go list -m github.com/cosmos/cosmos-sdk | sed  's/ /\@/g')
BINDIR ?= $(GOPATH)/bin
SIMAPP = ./app

# for dockerized protobuf tools
DOCKER := $(shell which docker)
HTTPS_GIT := github.com/pushchain/push-chain-node.git

export GO111MODULE = on

# don't override user values
ifeq (,$(VERSION))
  VERSION := $(shell git describe --tags --always)
  # if VERSION is empty, then populate it with branch's name and raw commit hash
  ifeq (,$(VERSION))
    VERSION := $(BRANCH)-$(COMMIT)
  endif
endif

# process build tags

build_tags = netgo
ifeq ($(LEDGER_ENABLED),true)
  ifeq ($(OS),Windows_NT)
    GCCEXE = $(shell where gcc.exe 2> NUL)
    ifeq ($(GCCEXE),)
      $(error gcc.exe not installed for ledger support, please install or set LEDGER_ENABLED=false)
    else
      build_tags += ledger
    endif
  else
    UNAME_S = $(shell uname -s)
    ifeq ($(UNAME_S),OpenBSD)
      $(warning OpenBSD detected, disabling ledger support (https://github.com/cosmos/cosmos-sdk/issues/1988))
    else
      GCC = $(shell command -v gcc 2> /dev/null)
      ifeq ($(GCC),)
        $(error gcc not installed for ledger support, please install or set LEDGER_ENABLED=false)
      else
        build_tags += ledger
      endif
    endif
  endif
endif

ifeq ($(WITH_CLEVELDB),yes)
  build_tags += gcc
endif
build_tags += $(BUILD_TAGS)
build_tags := $(strip $(build_tags))

whitespace :=
empty = $(whitespace) $(whitespace)
comma := ,
build_tags_comma_sep := $(subst $(empty),$(comma),$(build_tags))

# process linker flags

# flags '-s -w' resolves an issue with xcode 16 and signing of go binaries
# ref: https://github.com/golang/go/issues/63997
ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=pchain \
		  -X github.com/cosmos/cosmos-sdk/version.AppName=pchaind \
		  -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
		  -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
		  -X "github.com/cosmos/cosmos-sdk/version.BuildTags=$(build_tags_comma_sep)" \
		  -s -w

ifeq ($(WITH_CLEVELDB),yes)
  ldflags += -X github.com/cosmos/cosmos-sdk/types.DBBackend=cleveldb
endif
ifeq ($(LINK_STATICALLY),true)
	ldflags += -linkmode=external -extldflags "-Wl,-z,muldefs -static"
endif
ldflags += $(LDFLAGS)
ldflags := $(strip $(ldflags))

BUILD_FLAGS := -tags "$(build_tags_comma_sep)" -ldflags '$(ldflags)' -trimpath

# The below include contains the tools and runsim targets.
include contrib/devtools/Makefile

all: install lint test

build: go.sum
ifeq ($(OS),Windows_NT)
	$(error wasmd server not supported. Use "make build-windows-client" for client)
	exit 1
else
	go build -mod=readonly $(BUILD_FLAGS) -o build/pchaind ./cmd/pchaind
endif

build-windows-client: go.sum
	GOOS=windows GOARCH=amd64 go build -mod=readonly $(BUILD_FLAGS) -o build/pchaind.exe ./cmd/pchaind

# this does not compile because of WASM
build-linux-client: go.sum
	GOOS=linux GOARCH=amd64 go build -mod=readonly $(BUILD_FLAGS) -o build/pchaind ./cmd/pchaind

build-contract-tests-hooks:
ifeq ($(OS),Windows_NT)
	go build -mod=readonly $(BUILD_FLAGS) -o build/contract_tests.exe ./cmd/contract_tests
else
	go build -mod=readonly $(BUILD_FLAGS) -o build/contract_tests ./cmd/contract_tests
endif

install: go.sum
	go install -mod=readonly $(BUILD_FLAGS) ./cmd/pchaind

########################################
### Tools & dependencies

go-mod-cache: go.sum
	@echo "--> Download go modules to local cache"
	@go mod download

go.sum: go.mod
	@echo "--> Ensure dependencies have not been modified"
	@go mod verify

draw-deps:
	@# requires brew install graphviz or apt-get install graphviz
	go install github.com/RobotsAndPencils/goviz@latest
	@goviz -i ./cmd/pchaind -d 2 | dot -Tpng -o dependency-graph.png

clean:
	rm -rf snapcraft-local.yaml build/

distclean: clean
	rm -rf vendor/

########################################
### Testing

test: test-unit
test-all: test-race test-cover test-system

test-unit:
	@VERSION=$(VERSION) go test -mod=readonly -tags='ledger test_ledger_mock' ./...

test-race:
	@VERSION=$(VERSION) go test -mod=readonly -race -tags='ledger test_ledger_mock' ./...

test-cover:
	@go test -mod=readonly -timeout 30m -race -coverprofile=coverage.txt -covermode=atomic -tags='ledger test_ledger_mock' ./...

benchmark:
	@go test -mod=readonly -bench=. ./...

test-sim-import-export: runsim
	@echo "Running application import/export simulation. This may take several minutes..."
	@$(BINDIR)/runsim -Jobs=4 -SimAppPkg=$(SIMAPP) -ExitOnFail 50 5 TestAppImportExport

test-sim-multi-seed-short: runsim
	@echo "Running short multi-seed application simulation. This may take awhile!"
	@$(BINDIR)/runsim -Jobs=4 -SimAppPkg=$(SIMAPP) -ExitOnFail 50 5 TestFullAppSimulation

test-sim-deterministic: runsim
	@echo "Running application deterministic simulation. This may take awhile!"
	@$(BINDIR)/runsim -Jobs=4 -SimAppPkg=$(SIMAPP) -ExitOnFail 1 1 TestAppStateDeterminism

test-system: install
	$(MAKE) -C tests/system/ test

###############################################################################
###                                Linting                                  ###
###############################################################################

format-tools:
	go install mvdan.cc/gofumpt@v0.4.0
	go install github.com/client9/misspell/cmd/misspell@v0.3.4
	go install github.com/daixiang0/gci@v0.11.2

lint: format-tools
	golangci-lint run --tests=false
	find . -name '*.go' -type f -not -path "./vendor*" -not -path "./tests/system/vendor*" -not -path "*.git*" -not -path "*_test.go" | xargs gofumpt -d

format: format-tools
	find . -name '*.go' -type f -not -path "./vendor*" -not -path "./tests/system/vendor*" -not -path "*.git*" -not -path "./client/lcd/statik/statik.go" | xargs gofumpt -w
	find . -name '*.go' -type f -not -path "./vendor*" -not -path "./tests/system/vendor*" -not -path "*.git*" -not -path "./client/lcd/statik/statik.go" | xargs misspell -w
	find . -name '*.go' -type f -not -path "./vendor*" -not -path "./tests/system/vendor*" -not -path "*.git*" -not -path "./client/lcd/statik/statik.go" | xargs gci write --skip-generated -s standard -s default -s "prefix(cosmossdk.io)" -s "prefix(github.com/cosmos/cosmos-sdk)" -s "prefix(github.com/CosmWasm/wasmd)" --custom-order

mod-tidy:
	go mod tidy
	cd interchaintest && go mod tidy

.PHONY: format-tools lint format mod-tidy


###############################################################################
###                                Protobuf                                 ###
###############################################################################
CURRENT_UID := $(shell id -u)
CURRENT_GID := $(shell id -g)

protoVer=0.13.2
protoImageName=ghcr.io/cosmos/proto-builder:$(protoVer)
protoImage="$(DOCKER)" run -e BUF_CACHE_DIR=/tmp/buf --rm -v "$(CURDIR)":/workspace:rw --user ${CURRENT_UID}:${CURRENT_GID} --workdir /workspace $(protoImageName)

proto-all: proto-format proto-lint proto-gen format

proto-gen:
	@go install cosmossdk.io/orm/cmd/protoc-gen-go-cosmos-orm@v1.0.0-beta.3
	@echo "Generating Protobuf files"
	@$(protoImage) sh ./scripts/protocgen.sh
# generate the stubs for the proto files from the proto directory
	@spawn stub-gen
	@go mod tidy

proto-format:
	@echo "Formatting Protobuf files"
	@$(protoImage) find ./ -name "*.proto" -exec clang-format -i {} \;

proto-swagger-gen:
	@./scripts/protoc-swagger-gen.sh

proto-lint:
	@$(protoImage) buf lint --error-format=json

proto-check-breaking:
	@$(protoImage) buf breaking --against $(HTTPS_GIT)#branch=main

.PHONY: all install install-debug \
	go-mod-cache draw-deps clean build format \
	test test-all test-build test-cover test-unit test-race \
	test-sim-import-export build-windows-client \
	test-system

## --- Testnet Utilities ---
get-localic:
	@echo "Installing local-interchain"
	git clone --depth 1 --branch v8.7.0 https://github.com/strangelove-ventures/interchaintest.git interchaintest-downloader
	cd interchaintest-downloader/local-interchain && make install
	@sleep 0.1
	@echo ✅ local-interchain installed $(shell which local-ic)

is-localic-installed:
ifeq (,$(shell which local-ic))
	make get-localic
endif

get-heighliner:
	@echo ⏳ Installing heighliner...
	git clone --depth 1 https://github.com/strangelove-ventures/heighliner.git
	cd heighliner && go install
	@sleep 0.1
	@echo ✅ heighliner installed to $(shell which heighliner)

local-image:
ifeq (,$(shell which heighliner))
	echo 'heighliner' binary not found. Consider running `make get-heighliner`
else
	heighliner build -c pchain --local -f chains.yaml
endif

.PHONY: get-heighliner local-image is-localic-installed

###############################################################################
###                                     e2e                                 ###
###############################################################################

ictest-basic:
	@echo "Running basic e2e test"
	@cd interchaintest && go test -race -v -run TestBasicChain .

ictest-ibc:
	@echo "Running IBC e2e test"
	@cd interchaintest && go test -race -v -run TestIBCBasic .

ictest-wasm:
	@echo "Running cosmwasm e2e test"
	@cd interchaintest && go test -race -v -run TestCosmWasmIntegration .

ictest-packetforward:
	@echo "Running packet forward middleware e2e test"
	@cd interchaintest && go test -race -v -run TestPacketForwardMiddleware .

ictest-poa:
	@echo "Running proof of authority e2e test"
	@cd interchaintest && go test -race -v -run TestPOA .

ictest-tokenfactory:
	@echo "Running token factory e2e test"
	@cd interchaintest && go test -race -v -run TestTokenFactory .

ictest-ratelimit:
	@echo "Running rate limit e2e test"
	@cd interchaintest && go test -race -v -run TestIBCRateLimit .

ictest-blocktime:
	@echo "Running blocktime e2e test"
	@cd interchaintest && go test -race -v -run TestBlockTimeConfiguration .

ictest-gasfees:
	@echo "Running gas fees e2e test"
	@cd interchaintest && go test -race -v -run TestGasFees .

ictest-governance:
	@echo "Running governance e2e test"
	@cd interchaintest && go test -race -v -run TestGovernance .

ictest-tokentransfer:
	@echo "Running token transfer e2e test"
	@cd interchaintest && go test -race -v -run TestTokenTransfer .

###############################################################################
###                                    testnet                              ###
###############################################################################

setup-testnet: mod-tidy is-localic-installed install local-image set-testnet-configs setup-testnet-keys

# Run this before testnet keys are added
# This chain id is used in the testnet.json as well
set-testnet-configs:
	pchaind config set client chain-id localchain_9000-1
	pchaind config set client keyring-backend test
	pchaind config set client output text

# import keys from testnet.json into test keyring
setup-testnet-keys:
	-`echo "decorate bright ozone fork gallery riot bus exhaust worth way bone indoor calm squirrel merry zero scheme cotton until shop any excess stage laundry" | pchaind keys add acc0 --recover`
	-`echo "wealth flavor believe regret funny network recall kiss grape useless pepper cram hint member few certain unveil rather brick bargain curious require crowd raise" | pchaind keys add acc1 --recover`

testnet: setup-testnet
	spawn local-ic start testnet

sh-testnet: mod-tidy
	CHAIN_ID="localchain_9000-1" BLOCK_TIME="1000ms" CLEAN=true sh scripts/test_node.sh

.PHONY: setup-testnet set-testnet-configs testnet testnet-basic sh-testnet

###############################################################################
###                                     help                                ###
###############################################################################

.PHONY: generate-webapp
generate-webapp:
	sudo npm install --global create-cosmos-app
	cca --name web -e spawn

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Available targets:"
	@echo "  install             : Install the binary"
	@echo "  local-image         : Install the docker image"
	@echo "  proto-gen           : Generate code from proto files"
	@echo "  testnet             : Local devnet with IBC"
	@echo "  sh-testnet          : Shell local devnet"
	@echo "  ictest-basic        : Basic end-to-end test"
	@echo "  ictest-ibc          : IBC end-to-end test"
	@echo "  generate-webapp     : Create a new webapp template"

.PHONY: help

###############################################################################
###                                     e2e...                              ###
###############################################################################

# ------------------------
# Docker commands (from before)
# ------------------------
docker-build:
	docker build -t push-chain-node -f Dockerfile.e2e .

docker-up:
	docker compose -f docker-compose.yml up --build -d

docker-down:
	docker compose -f docker-compose.yml down

docker-logs:
	docker compose -f docker-compose.yml logs -f

docker-reset:
	docker system prune -a --volumes -f


# ------------------------
# Docker Setup flow
# ------------------------

ANVIL_URL=http://anvil:9545
PUSH_EVM_URL=http://localhost:8545
CHAIN_RPC=http://localhost:26657
CHAIN_ID=localchain_9000-1

# Path where contracts will be cloned
CONTRACTS_DIR := contracts-tmp
INTEROP_REPO := https://github.com/pushchain/push-chain-interop-contracts.git/
CORE_REPO := https://github.com/pushchain/push-chain-core-contracts.git
SDK_REPO := https://github.com/AryaLanjewar3005/push-chain-sdk.git
E2E_DIR := e2e

e2e: docker-up wait-for-services fund-acc1 deploy-interop deploy-core e2e-solana-interop-deployment e2e-solana-chain-config e2e-run-test

# Wait for services to start up
wait-for-services:
	@echo "Waiting for Anvil and Push-Chain-Node to start..."
	@for i in {1..30}; do \
		if docker exec push-chain-node curl -s --fail http://localhost:26657/status; then \
			echo "Push-Chain Node is ready"; \
			break; \
		fi; \
		echo "Waiting for Push-Chain Node..."; \
		sleep 2; \
	done
	docker logs push-chain-node

# Fund acc1 on push-chain
fund-acc1:
	@echo "Funding acc1 on push-chain..."
	docker exec push-chain-node pchaind tx bank send push1j0v5urpud7kwsk9zgz2tc0v9d95ct6t5qxv38h \
		push1w7xnyp3hf79vyetj3cvw8l32u6unun8yr6zn60 \
		1000000000000000000upc \
		--gas-prices 100000000000upc -y

# Deploy the interop contract and capture address
deploy-interop:
		echo "Adding Sepolia config to push-chain" && \
		docker exec -it push-chain-node pchaind tx uexecutor add-chain-config \
			--chain-config "{\"chain\":\"eip155:11155111\",\"public_rpc_url\":\"http://anvil:9545\",\"vm_type\":0,\"gateway_address\":\"0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe\",\"block_confirmation\":0,\"gateway_methods\":[{\"name\":\"addFunds\",\"identifier\":\"0xf9bfe8a7\",\"event_identifier\":\"0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd\"}],\"enabled\":true}" \
			--from acc1 \
			--gas-prices 100000000000upc -y

# Deploy push-core-contracts using forge script
deploy-core:
	@echo "Deploying Push Core Contracts..."
	@rm -rf $(CONTRACTS_DIR) && mkdir $(CONTRACTS_DIR)
	cd $(CONTRACTS_DIR) && git clone $(CORE_REPO)
	cd $(CONTRACTS_DIR)/push-chain-core-contracts && git submodule update --init --recursive
	cd $(CONTRACTS_DIR)/push-chain-core-contracts && forge install && forge build
	cd $(CONTRACTS_DIR)/push-chain-core-contracts && forge script scripts/deployFactory.s.sol \
		--broadcast \
		--rpc-url $(PUSH_EVM_URL) \
		--private-key 0x0dfb3d814afd8d0bf7a6010e8dd2b6ac835cabe4da9e2c1e80c6a14df3994dd4 \
		--slow

	cd $(CONTRACTS_DIR)/push-chain-core-contracts && forge script scripts/deployMock.s.sol --broadcast --rpc-url http://localhost:8545 --private-key 0x0dfb3d814afd8d0bf7a6010e8dd2b6ac835cabe4da9e2c1e80c6a14df3994dd4 --slow

e2e-solana-chain-config:
	echo "Adding Solana config to push-chain"
	docker exec -it push-chain-node pchaind tx uexecutor add-chain-config --chain-config "$$(cat e2e/solana_localchain_chain_config.json)" --from acc1 --gas-prices 100000000000upc -y
	
e2e-solana-interop-deployment:
	@echo "Setting Solana CLI to local validator..."
	cd $(E2E_DIR) && rm -rf push-chain-interop-contracts
	solana config set --url http://127.0.0.1:8899
	@echo "Deploying svm_gateway contract on solana-test-validator"
	cp $(E2E_DIR)/.env.sample $(E2E_DIR)/.env 
	cd $(E2E_DIR) && git clone $(INTEROP_REPO)
	cp $(E2E_DIR)/deploy.sh $(E2E_DIR)/push-chain-interop-contracts/contracts/svm-gateway/deploy.sh
	cd $(E2E_DIR)/push-chain-interop-contracts/contracts/svm-gateway && ./deploy.sh localnet
	
	@echo "Initializing account and funding vault..."
	cd $(E2E_DIR)/solana-setup && npm install
	  
	@cd $(E2E_DIR)/solana-setup && VAULT=$$(npx ts-node --compiler-options '{"module":"commonjs"}' ./index.ts | grep 'vaultbalance' | awk '{print $$2}'); \
	echo "Vault PDA is $$VAULT"; \
	solana airdrop 10 $$VAULT --url http://127.0.0.1:8899
	

e2e-run-test:
	@echo "Cloning e2e repository..."
	@rm -rf $(CONTRACTS_DIR)/push-chain-sdk
	cd $(CONTRACTS_DIR) && git clone $(SDK_REPO)
	cd $(CONTRACTS_DIR)/push-chain-sdk && git checkout push-node-e2e && yarn install
	cp $(E2E_DIR)/push-chain-interop-contracts/contracts/svm-gateway/target/idl/pushsolanalocker.json $(CONTRACTS_DIR)/push-chain-sdk/packages/core/src/lib/constants/abi/feeLocker.json
	cp $(E2E_DIR)/.env $(CONTRACTS_DIR)/push-chain-sdk/packages/core/.env
	cd $(CONTRACTS_DIR)/push-chain-sdk && npx jest core/__e2e__/pushchain.spec.ts --runInBand --detectOpenHandles
