#!/bin/bash
echo "Building sources for linux:amd64 into release/"
(cd .. && ignite chain build --release.targets linux:amd64 --output ./release --release)