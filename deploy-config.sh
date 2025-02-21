#!/bin/bash
SRC_HOME_DIR="/Users/igx/.tn/pn1"
REMOTE_HOST="pn1.dev.push.org"

# DO NOT EDIT THIS UNLESS NEEDED
DEST_HOME_DIR="/home/igx/.push"
ARCHIVE_FILE="home.tar.gz"          # Per instruction, although typically tar.gz is used.
REMOTE_ARCHIVE="/tmp/${ARCHIVE_FILE}"

echo "Creating archive $ARCHIVE_FILE from $SRC_HOME_DIR..."
COPYFILE_DISABLE=1 tar -czvf "$ARCHIVE_FILE" -C "$SRC_HOME_DIR" .
if [ $? -ne 0 ]; then
    echo "Error: Archiving $SRC_HOME_DIR failed."
    exit 1
fi
echo "Uploading $ARCHIVE_FILE to $REMOTE_HOST:$REMOTE_ARCHIVE..."
scp "$ARCHIVE_FILE" "$REMOTE_HOST:$REMOTE_ARCHIVE"
if [ $? -ne 0 ]; then
    echo "Error: SCP of $ARCHIVE_FILE failed."
    exit 1
fi

echo "Extracting $ARCHIVE_FILE on $REMOTE_HOST into $DEST_HOME_DIR..."
ssh "$REMOTE_HOST" "mkdir -p '$DEST_HOME_DIR' && tar -xzvf '$REMOTE_ARCHIVE' -C '$DEST_HOME_DIR'"
if [ $? -ne 0 ]; then
    echo "Error: Extraction of $ARCHIVE_FILE failed."
    exit 1
fi

echo "Deployment complete."