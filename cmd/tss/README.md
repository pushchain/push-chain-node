# TSS Demo

## Quick Start

Start 3 nodes in separate terminals:

```bash
# Terminal 1
./scripts/test_tss.sh pushvaloper1fv2fm76q7cjnr58wdwyntzrjgtc7qya6n7dmlu 39001 30B0D912700C3DF94F4743F440D1613F7EA67E1CEF32C73B925DB6CD7F1A1544

# Terminal 2
./scripts/test_tss.sh pushvaloper12jzrpp4pkucxxvj6hw4dfxsnhcpy6ddty2fl75 39002 59BA39BF8BCFE835B6ABD7FE5208D8B8AEFF7B467F9FE76F1F43ED392E5B9432

# Terminal 3
./scripts/test_tss.sh pushvaloper1vzuw2x3k2ccme70zcgswv8d88kyc07grdpvw3e 39003 957590C7179F8645368162418A3DF817E5663BBC7C24D0EFE1D64EFFB11DC595
```

Trigger keygen:

```bash
./build/tss keygen -key-id=test-key-1
```

The test script automatically builds the binary, cleans up old data, and logs to `/tmp/tss-<validator>.log`.

## Commands

- `keygen` - Generate a new keyshare (key-id is optional, auto-generated if not provided)
