#!/bin/bash
echo "🛑 Stopping node..."
pkill -f "pchaind start" || echo "No node running."
