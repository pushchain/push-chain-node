```app/
├── binary/            # Compiled binary lives here
│   └── pchaind
├── scripts/           # start, stop, log, etc.
│   ├── start.sh
│   ├── stop.sh
│   └── log.sh
├── setup/             # One-time setup scripts (e.g., genesis or validator add)
│   └── setup_genesis_validator.sh
├── logs/              # Log files (logrotate-friendly)
│   └── pchaind.log
└── .pchain/           # Cosmos node data (config/, data/, keys/, etc.)
```
