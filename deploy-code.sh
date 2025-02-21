#!/bin/bash
FILE="release/push_linux_amd64.tar.gz"
REMOTE_HOST="pn1.dev.push.org"
DEST_EXEC_DIR="/home/igx/app"


# DO NOT EDIT THIS UNLESS NEEDED

echo "Building sources for linux:amd64 into release/"
ignite chain build --release.targets linux:amd64 --output ./release --release

REMOTE_TMP="/tmp/$(basename "$FILE")"
echo "Transferring $FILE to $REMOTE_HOST:$REMOTE_TMP..."
scp "$FILE" "$REMOTE_HOST:$REMOTE_TMP"
if [ $? -ne 0 ]; then
    echo "Error: SCP failed."
    exit 1
fi
echo "Extracting the tarball on $REMOTE_HOST into $DEST_EXEC_DIR..."
ssh "$REMOTE_HOST" "mkdir -p '$DEST_EXEC_DIR' && tar -xzvf '$REMOTE_TMP' -C '$DEST_EXEC_DIR' && chmod u+x ~/app/pushchaind"
if [ $? -ne 0 ]; then
    echo "Error: Extraction failed."
    exit 1
fi

echo "Deployment complete."
