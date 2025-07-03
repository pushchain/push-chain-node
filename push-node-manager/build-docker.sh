#!/bin/bash

# Multi-Architecture Docker Build Script for Push Node Manager
# Supports both Mac (ARM64/AMD64) and Linux (AMD64/ARM64)

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Configuration
IMAGE_NAME="push-node-manager"
BINARY_VERSION="${BINARY_VERSION:-v1.1.0}"

# Detect current platform
CURRENT_ARCH=$(uname -m)
CURRENT_OS=$(uname -s | tr '[:upper:]' '[:lower:]')

case "$CURRENT_ARCH" in
    "x86_64") CURRENT_ARCH="amd64" ;;
    "arm64"|"aarch64") CURRENT_ARCH="arm64" ;;
esac

CURRENT_PLATFORM="${CURRENT_OS}/${CURRENT_ARCH}"

print_status "🐋 Multi-Architecture Docker Build for Push Node Manager"
print_status "Current platform: $CURRENT_PLATFORM"
print_status "Binary version: $BINARY_VERSION"

# Function to check if buildx is available
check_buildx() {
    if ! docker buildx version >/dev/null 2>&1; then
        print_error "Docker buildx is not available!"
        print_error "Please install Docker Desktop or enable buildx"
        exit 1
    fi
    print_success "Docker buildx is available"
}

# Function to setup buildx builder
setup_builder() {
    print_status "Setting up multi-architecture builder..."
    
    # Create or use existing builder
    if ! docker buildx inspect multiarch >/dev/null 2>&1; then
        print_status "Creating new multiarch builder..."
        docker buildx create --name multiarch --driver docker-container --use
    else
        print_status "Using existing multiarch builder..."
        docker buildx use multiarch
    fi
    
    # Bootstrap the builder
    print_status "Bootstrapping builder..."
    docker buildx inspect --bootstrap
    
    print_success "Builder ready!"
}

# Function to build for specific platform
build_platform() {
    local platform=$1
    local tag_suffix=$2
    
    print_status "🔨 Building for platform: $platform"
    
    docker buildx build \
        --platform "$platform" \
        --build-arg BINARY_VERSION="$BINARY_VERSION" \
        --tag "${IMAGE_NAME}:${tag_suffix}" \
        --load \
        .
    
    print_success "✅ Built ${IMAGE_NAME}:${tag_suffix}"
}

# Function to build multi-arch and push
build_multiarch() {
    print_status "🔨 Building multi-architecture image..."
    
    docker buildx build \
        --platform linux/amd64,linux/arm64 \
        --build-arg BINARY_VERSION="$BINARY_VERSION" \
        --tag "${IMAGE_NAME}:latest" \
        --tag "${IMAGE_NAME}:${BINARY_VERSION}" \
        --push \
        .
    
    print_success "✅ Built and pushed multi-architecture image"
}

# Function to test the built image
test_image() {
    local tag=$1
    
    print_status "🧪 Testing image: ${IMAGE_NAME}:${tag}"
    
    if docker run --rm "${IMAGE_NAME}:${tag}" pchaind version >/dev/null 2>&1; then
        print_success "✅ Image test passed"
    else
        print_warning "⚠️  Image test failed (may be due to architecture mismatch)"
    fi
}

# Main execution
case "${1:-local}" in
    "local")
        print_status "Building for current platform: $CURRENT_PLATFORM"
        check_buildx
        
        case "$CURRENT_PLATFORM" in
            "darwin/arm64")
                build_platform "linux/arm64" "mac-arm64"
                test_image "mac-arm64"
                ;;
            "darwin/amd64")
                build_platform "linux/amd64" "mac-amd64"
                test_image "mac-amd64"
                ;;
            "linux/amd64")
                build_platform "linux/amd64" "linux-amd64"
                test_image "linux-amd64"
                ;;
            "linux/arm64")
                build_platform "linux/arm64" "linux-arm64"
                test_image "linux-arm64"
                ;;
            *)
                print_warning "Unknown platform: $CURRENT_PLATFORM, building for linux/amd64"
                build_platform "linux/amd64" "default"
                test_image "default"
                ;;
        esac
        ;;
        
    "multiarch")
        print_status "Building multi-architecture image for registry"
        check_buildx
        setup_builder
        build_multiarch
        ;;
        
    "all")
        print_status "Building for all supported platforms"
        check_buildx
        setup_builder
        
        build_platform "linux/amd64" "linux-amd64"
        build_platform "linux/arm64" "linux-arm64"
        
        print_success "✅ All platform builds completed"
        ;;
        
    "clean")
        print_status "Cleaning up builders and images"
        docker buildx rm multiarch 2>/dev/null || true
        docker image prune -f
        print_success "✅ Cleanup completed"
        ;;
        
    "help"|*)
        echo ""
        echo "🐋 Multi-Architecture Docker Build Script"
        echo "=========================================="
        echo ""
        echo "Usage: ./build-docker.sh <command>"
        echo ""
        echo "Commands:"
        echo "  local      Build for current platform (default)"
        echo "  multiarch  Build multi-arch image and push to registry"
        echo "  all        Build for all supported platforms locally"
        echo "  clean      Clean up builders and images"
        echo "  help       Show this help message"
        echo ""
        echo "Environment Variables:"
        echo "  BINARY_VERSION  Binary version to use (default: v1.1.0)"
        echo ""
        echo "Examples:"
        echo "  ./build-docker.sh local              # Build for Mac/current platform"
        echo "  ./build-docker.sh all                # Build all platforms"
        echo "  BINARY_VERSION=v1.2.0 ./build-docker.sh local"
        echo ""
        ;;
esac 