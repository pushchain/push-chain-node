#!/bin/bash



if [ -z "$REMOTE_HOST" ]; then
  echo "REMOTE_HOST var is missing"
  exit 1
fi
echo "REMOTE_HOST is $REMOTE_HOST"

# DO NOT EDIT THIS UNLESS NEEDED
DEST_HOME_DIR="/home/chain/.push"
PROCESS_NAME="pushchaind"
 # !!!!!!!!!!!!!!!!!!!!!!!!!!!! SUDO as chain, kill, then, start!
echo "Stopping app on $REMOTE_HOST"
ssh "$REMOTE_HOST" "pkill -f -e -c $PROCESS_NAME"

echo "Starting app on $REMOTE_HOST"
#??????????????ssh "$REMOTE_HOST" "pkill -f -e -c $PROCESS_NAME"

if [ $? -ne 0 ]; then
    echo "Error: Extraction failed."
    exit 1
fi

echo "Deployment complete."
