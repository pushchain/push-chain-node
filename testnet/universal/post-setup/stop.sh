#!/bin/bash
echo "🛑 Stopping node..."
pkill -f "puniversald start" || echo "No Universal Node running."
