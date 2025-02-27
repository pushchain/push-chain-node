#!/bin/bash

if [ -z "$REMOTE_HOST" ]; then
  echo "REMOTE_HOST var is missing"
  exit 1
fi
echo "REMOTE_HOST is $REMOTE_HOST"

# DO NOT EDIT THIS UNLESS NEEDED
FILE="../release/push_linux_amd64.tar.gz"
DEST_EXEC_DIR="/home/chain/app"
REMOTE_TMP="/tmp/$(basename "$FILE")"
echo "Transferring $FILE to $REMOTE_HOST:$REMOTE_TMP..."
scp "$FILE" "$REMOTE_HOST:$REMOTE_TMP"
if [ $? -ne 0 ]; then
    echo "Error: SCP failed."
    exit 1
fi
echo "Extracting the tarball on $REMOTE_HOST into $DEST_EXEC_DIR..."
ssh "$REMOTE_HOST" "mkdir -p '$DEST_EXEC_DIR' && tar -xzvf '$REMOTE_TMP' -C '$DEST_EXEC_DIR' && chmod u+x $DEST_EXEC_DIR/pushchaind"
if [ $? -ne 0 ]; then
    echo "Error: Extraction failed."
    exit 1
fi

echo "Deployment complete."
