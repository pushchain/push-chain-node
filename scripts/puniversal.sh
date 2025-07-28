#!/bin/bash

# Universal Validator Setup Script
# This script is specifically for puniversald binary

set -e

# Default values
export HOME_DIR=${HOME_DIR:-"$HOME/.puniversal"}
export CLEAN=${CLEAN:-"true"}

echo "Setting up puniversald..."

# Check if binary exists, install if not
if [ -z `which puniversald` ]; then
  echo "Installing puniversald..."
  make install
  if [ -z `which puniversald` ]; then
    echo "Ensure puniversald is installed and in your PATH"
    exit 1
  fi
fi

# Clean setup if requested
if [ "$CLEAN" != "false" ]; then
  echo "Starting from a clean state"
  echo "Removing $HOME_DIR"
  rm -rf $HOME_DIR
  # Also clean the database file to prevent schema migration conflicts
  echo "Removing database file"
  rm -f $HOME/.puniversal/data/pushuv.db
fi

echo "Initializing puniversald..."
puniversald init

echo "Starting puniversald..."
puniversald start 