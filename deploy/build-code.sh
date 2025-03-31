#!/bin/bash
CUR_DIR=$(pwd)
cd .. || exit
make install
cp "$HOME/go/bin/pchaind" build/pchaind
cd "$CUR_DIR" || exit