#!/bin/bash

###############################################
# Push Chain Node Stop Script (Cosmovisor)
#
# Stops the Cosmovisor-managed node process.
###############################################

echo "🛑 Stopping Cosmovisor-managed node..."
pkill -f "cosmovisor run" || echo "No Cosmovisor process running."
# Also stop any direct pchaind processes
pkill -f "pchaind start" || echo "No direct pchaind process running."
echo "✅ Stop command completed."
