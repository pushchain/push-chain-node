#!/bin/bash

if [ -z "$REMOTE_HOST" ]; then
  echo "REMOTE_HOST var is missing"
  exit 1
fi
echo "REMOTE_HOST is $REMOTE_HOST"

if [ -z "$CONFIG_HOME_DIR" ]; then
  echo "CONFIG_HOME_DIR var is missing"
  exit 1
fi
echo "CONFIG_HOME_DIR is $CONFIG_HOME_DIR"


# DO NOT EDIT THIS UNLESS NEEDED
DEST_HOME_DIR="/home/chain/.push"
ARCHIVE_FILE="config.tar.gz"
REMOTE_ARCHIVE="/tmp/${ARCHIVE_FILE}"

echo "Creating archive $ARCHIVE_FILE from $CONFIG_HOME_DIR..."
COPYFILE_DISABLE=1 tar --exclude=*.sample -czvf "$ARCHIVE_FILE" -C "$CONFIG_HOME_DIR" .
if [ $? -ne 0 ]; then
    echo "Error: Archiving $CONFIG_HOME_DIR failed."
    exit 1
fi
echo "Uploading $ARCHIVE_FILE to $REMOTE_HOST:$REMOTE_ARCHIVE..."
scp "$ARCHIVE_FILE" "$REMOTE_HOST:$REMOTE_ARCHIVE"
if [ $? -ne 0 ]; then
    echo "Error: SCP of $ARCHIVE_FILE failed."
    exit 1
fi

# TODO find a way to avoid: sudo chown, sudo tar. I need to ssh as chain@host in order to do things correctly;
echo "Extracting $ARCHIVE_FILE on $REMOTE_HOST into $DEST_HOME_DIR..."
ssh "$REMOTE_HOST" "mkdir -p '$DEST_HOME_DIR' && sudo tar --no-same-owner --no-same-permissions -xzvf '$REMOTE_ARCHIVE' -C '$DEST_HOME_DIR'"
ssh "$REMOTE_HOST" "sudo chown -hR chain:devops '$DEST_HOME_DIR'"
if [ $? -ne 0 ]; then
    echo "Error: Extraction of $ARCHIVE_FILE failed."
    exit 1
fi

echo "Deployment complete."