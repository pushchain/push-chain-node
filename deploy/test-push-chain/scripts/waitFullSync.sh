#!/bin/bash
while true; do

  latest=$($HOME/app/pushchaind status | jq -r '.sync_info.latest_block_height')
  catching=$($HOME/app/pushchaind status | jq -r '.sync_info.catching_up')
  echo "\"latest_block_height\":\"$latest\""
  echo "\"catching_up\":$catching"
  if [ "$catching" = "false" ]; then
    echo "The Node has been fully synced !!!"
    break
  fi
  sleep 10
done
