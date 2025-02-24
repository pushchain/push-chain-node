#!/bin/bash
SRC_HOME_DIR="/Users/igx/.tn/pn2"
REMOTE_HOST="pn2.dev.push.org"
REMOTE_DIR="/home/igx/.push/"
KEY_FILE="user2_key.json"

# DO NOT EDIT THIS UNLESS NEEDED

echo "Exporting key..."
pchaind keys export user2 --output json > $KEY_FILE

echo "Uploading $KEY_FILE to $REMOTE_HOST:$REMOTE_DIR..."
scp "$KEY_FILE" "$REMOTE_HOST:$REMOTE_DIR$KEY_FILE"
if [ $? -ne 0 ]; then
    echo "Error: SCP of $KEY_FILE failed."
    exit 1
fi