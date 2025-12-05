# Docker Bake configuration for parallel builds with caching
# Usage: docker buildx bake -f docker-bake.hcl [target]

# Build groups
group "default" {
  targets = ["base", "core", "universal"]
}

group "runtime" {
  targets = ["core", "universal"]
}

# Base image with Rust/Go dependencies (build first, rarely changes)
target "base" {
  context = "../.."
  dockerfile = "push-chain/local-multi-validator/Dockerfile.base"
  tags = ["local-multi-validator-base:latest"]
  cache-from = ["type=local,src=/tmp/.buildx-cache/base"]
  cache-to = ["type=local,dest=/tmp/.buildx-cache/base,mode=max"]
}

# Core validator runtime (pchaind)
target "core" {
  context = "../.."
  dockerfile = "push-chain/local-multi-validator/Dockerfile.unified"
  target = "core-runtime"
  tags = ["push-core:latest"]
  contexts = {
    "local-multi-validator-base:latest" = "target:base"
  }
  cache-from = ["type=local,src=/tmp/.buildx-cache/unified"]
  cache-to = ["type=local,dest=/tmp/.buildx-cache/unified,mode=max"]
}

# Universal validator runtime (puniversald)
target "universal" {
  context = "../.."
  dockerfile = "push-chain/local-multi-validator/Dockerfile.unified"
  target = "universal-runtime"
  tags = ["push-universal:latest"]
  contexts = {
    "local-multi-validator-base:latest" = "target:base"
  }
  cache-from = ["type=local,src=/tmp/.buildx-cache/unified"]
  cache-to = ["type=local,dest=/tmp/.buildx-cache/unified,mode=max"]
}
