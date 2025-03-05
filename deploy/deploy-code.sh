#!/bin/bash

LOCAL_FILE="$1"
REMOTE_HOST="$2"
DEST_DIR="$3"

if [ -z "$LOCAL_FILE" ]; then
  echo "LOCAL_FILE var is missing"
  exit 1
fi
echo "LOCAL_FILE is $LOCAL_FILE"

if [ -z "$REMOTE_HOST" ]; then
  echo "REMOTE_HOST var is missing"
  exit 1
fi
echo "REMOTE_HOST is $REMOTE_HOST"

if [ -z "$DEST_DIR" ]; then
  echo "DEST_DIR var is missing"
  exit 1
fi
echo "DEST_DIR is $DEST_DIR"

# DO NOT EDIT THIS UNLESS NEEDED
LOCAL_FILE=${LOCAL_FILE:-"../release/push_linux_amd64.tar.gz"}
DEST_DIR="~/app"
REMOTE_TMP="/tmp/$(basename "$LOCAL_FILE")"
echo "Transferring $LOCAL_FILE to $REMOTE_HOST:$REMOTE_TMP..."
scp "$LOCAL_FILE" "$REMOTE_HOST:$REMOTE_TMP"
if [ $? -ne 0 ]; then
    echo "Error: SCP failed."
    exit 1
fi
echo "Extracting the tarball on $REMOTE_HOST into $DEST_DIR..."
ssh "$REMOTE_HOST" "mkdir -p '$DEST_DIR' && tar -xzvf '$REMOTE_TMP' -C '$DEST_DIR' && chmod u+x $DEST_DIR/pushchaind"
if [ $? -ne 0 ]; then
    echo "Error: Extraction failed."
    exit 1
fi

echo "Deployment complete."
