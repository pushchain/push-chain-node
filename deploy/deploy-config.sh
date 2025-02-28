#!/bin/bash

HDIR="$1"
REMOTE_HOST="$2"
DEST_HOME_DIR="$3"

if [ -z "$REMOTE_HOST" ]; then
  echo "REMOTE_HOST var is missing"
  exit 1
fi
echo "REMOTE_HOST is $REMOTE_HOST"

if [ -z "$HDIR" ]; then
  echo "$HDIR var is missing"
  exit 1
fi
echo "HDIR is $HDIR"

if [ -z "$DEST_HOME_DIR" ]; then
  echo "$DEST_HOME_DIR var is missing"
  exit 1
fi
echo "DEST_HOME_DIR is $DEST_HOME_DIR"

# DO NOT EDIT THIS UNLESS NEEDED
ARCHIVE_FILE="home.tar.gz"
REMOTE_ARCHIVE="/tmp/${ARCHIVE_FILE}"

echo "Creating archive $ARCHIVE_FILE from $HDIR..."
COPYFILE_DISABLE=1 tar --exclude=*.sample -czvf "$ARCHIVE_FILE" -C "$HDIR" .
if [ $? -ne 0 ]; then
    echo "Error: Archiving $HDIR failed."
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
ssh "$REMOTE_HOST" "sudo chown -hR chain:devops '$DEST_HOME_DIR'"
ssh "$REMOTE_HOST" "mkdir -p '$DEST_HOME_DIR' && sudo tar --no-same-owner --no-same-permissions -xzvf '$REMOTE_ARCHIVE' -C '$DEST_HOME_DIR'"
ssh "$REMOTE_HOST" "sudo chmod g+rw -R '$DEST_HOME_DIR'"
ssh "$REMOTE_HOST" "sudo chmod o-rwx -R '$DEST_HOME_DIR'"
if [ $? -ne 0 ]; then
    echo "Error: Extraction of $ARCHIVE_FILE failed."
    exit 1
fi

echo "Deployment complete."