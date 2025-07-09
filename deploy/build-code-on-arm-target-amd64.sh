#!/bin/bash
echo "Building sources for linux:amd64 into ./release/pchaind inside a container"
CUR_DIR=$(pwd)
cd .. || exit
mkdir -p build
docker buildx build --platform linux/amd64 -t pnode-main --load .
docker create --platform linux/amd64 --name tmp pnode-main
mkdir -p build # Create build directory in the project root
docker cp tmp:/usr/bin/pchaind build/pchaind
# should print x64
file build/pchaind
docker rm -f tmp 2>/dev/null || true
cd "$CUR_DIR" || exit