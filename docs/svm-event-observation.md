# SVM event observation: current state and options

Context for F-2026-18198 (forged gateway events) and the log truncation gap found alongside it.
Covers how Solana event observation differs from EVM, what we shipped, and what the remaining
options are.

## Why Solana is different from EVM

**EVM.** One filtered call does everything:

```go
query := ethereum.FilterQuery{Addresses: []common.Address{gateway, vault}, Topics: ...}
logs, _ := rpcClient.FilterLogs(ctx, query)
```

A log's `address` field is set by the EVM itself when the contract executes `LOG0`-`LOG4`.
A contract cannot emit a log attributed to another address, so attribution is a protocol
guarantee and we get it for free. `topic[0]` identifies the event.

**Solana.** There is no event system. What Anchor calls an event is `sol_log_data(bytes)`,
which appends a base64 string to a flat `meta.logMessages` array:

```
Program <gateway> invoke [1]
Program log: Instruction: SendFunds
Program data: <base64>          <- the "event"
Program <gateway> success
```

Two consequences:

- No per-log program field. Nothing in that line says who emitted it. The only signal is
  which `invoke` frame was open at the time.
- No "get logs by program" RPC. `getSignaturesForAddress` returns transactions that merely
  reference an address in `accountKeys`. Referenced, not invoked, not even writable.

So on Solana attribution must be reconstructed. That reconstruction was missing, which is
what F-2026-18198 exploited.

## What was wrong

We applied the EVM mental model:

1. `getSignaturesForAddress(gateway)` to select candidate transactions.
2. Treat every `Program data:` line whose 8-byte discriminator matched as a gateway event.

Both steps were unsound. Referencing the gateway proves nothing, and a discriminator is
`sha256("event:SendFunds")[:8]`, a public schema tag rather than an authenticator. Any
program could emit a well-formed `send_funds` event with any amount and recipient. Every
honest UV would read the same successful transaction and vote identically, minting PRC20
with no deposit behind it. No validator compromise required.

## What we shipped (PR #308)

**1. Invocation-stack attribution.** `gatewayEmittedLogs` walks the runtime `invoke` and
`success` / `failed:` lines as a stack and accepts a `Program data:` line only while the
gateway is the executing frame.

Sound because programs cannot forge those lines: `sol_log` always produces `Program log: `
and `sol_log_data` always produces `Program data: `, so bare `Program <id> invoke [n]` lines
are runtime generated. Tested, not assumed.

A first attempt had its own hole: `isProgramExit("Program log: success")` returned true, so a
program logging `msg!("success")` could pop a frame it did not own. If the gateway CPIs into
another program, that callee could pop its own frame and have its next data log attributed to
the gateway. Fixed by requiring the token to parse as a base58 pubkey.

**2. Truncation detection.** `logsTruncated` flags a dropped log buffer and the listener logs
at error level with signature and slot. Visible logs are still processed, since events before
the cut are genuine.

**3. Dropped-event identification.** `gatewayInstructionCounts` counts the gateway instructions
that executed and compares them against the events observed, so a shortfall names the event type
and count rather than just reporting that the buffer was cut. Option B1 below.

This is identification, not recovery. See below.

## The remaining gap: log truncation

Solana caps the log buffer per transaction. On overflow the runtime drops the remainder and
appends a truncation marker. Because gateway events are emitted with `sol_log_data`, an
overflowing transaction can lose the event line entirely.

Impact is liveness, not forgery: a real deposit is never observed, and the user's funds sit on
Solana until someone reconciles manually. No RPC call recovers a truncated log.

Rarity, measured: 7116 mainnet transactions scanned across 5 blocks, zero truncated. Rare,
but the consequence per occurrence is a stuck deposit.

## Options for closing it

### Option A: detection only (shipped)

Flag truncation at error level so a missed deposit is alertable instead of silent.

- Cost: done.
- Closes forgery: not applicable, separate fix.
- Closes truncation: no. Converts silent loss into detected loss.
- Requires Solana program change: no.

### Option B: use the instruction, not the event log (recommended)

Instructions live in `transaction.message.instructions` and `meta.innerInstructions`. Neither
is part of the log buffer, so neither is ever truncated. This splits into two steps of very
different cost.

**B1: detect a dropped event (shipped in this PR).** Count the gateway instructions carrying a
known instruction discriminator, compare against the events actually observed, and report a
shortfall. This needs only the discriminator, not the argument layout. It converts "the buffer
was cut somewhere" into "this event type was lost, N times, in this signature".

Only the shortfall direction is checked. `finalize_universal_tx` emits more than one event per
instruction (see below), so requiring equality would false-positive.

**B2: reconstruct the event (needs the IDL).** The event is a deterministic function of the
instruction arguments and the account list, so it can be rebuilt when the log is gone. This is
verified, not assumed. Field alignment from a live devnet transaction
(`3FuFyisoxYwvwsbXhEX9Z6kg…`), 108 argument bytes against a 138 byte event body:

| Event offset | Content | Source |
| --- | --- | --- |
| `[0:32]` | pubkey | `accounts[5]`, also `ix[64:96]` |
| `[32:52]` | EVM recipient | `ix[0:20]` |
| `[52:84]` | 32 bytes | `ix[20:52]` |
| `[84:92]` | amount, u64 LE | `ix[52:60]` |
| `[92:96]` | borsh string `"PRC2"` | program supplied |
| `[100:132]` | pubkey | `accounts[5]`, also `ix[64:96]` |

Every field except the token-standard string comes straight from the instruction or the
accounts. So reconstruction is feasible, and it would make truncation a non-issue for inbound
deposits rather than merely detectable.

It should be built as a shadow check: on every transaction where the event *is* present,
reconstruct it as well and compare. Any disagreement is logged and the logged event wins. That
proves the decoder continuously against production traffic, and the reconstructed value is only
consumed when the log is genuinely missing.

**Blocker.** The three samples available were near identical (same accounts, same amount, one
all-zero field), which is far too narrow to pin a Borsh layout. Inferring the rest is the same
class of unverified assumption that produced the original bug. This needs the gateway IDL or
program source, which is not in this repository. That is now a small, concrete ask rather than
an open question.

- Cost: B1 done. B2 medium, client only.
- Closes truncation: B1 detects, B2 recovers.
- Requires Solana program change: no.

### Blocking prerequisite: the registry identifiers are wrong

B1 is inert until the registry is corrected. Verified against devnet
(`pchaind q uregistry all-chain-configs --node https://donut.rpc.push.org`) and against 50 live
gateway transactions:

| Method | Registry `identifier` | Actually executing on devnet |
| --- | --- | --- |
| `send_funds` | `54f7d3283f6a0f3b` = `global:send_funds` | `9113a437a5dc1761` = `global:send_universal_tx` |
| `finalize_universal_tx` | `0x` | `de5bee964bd80250` = `global:finalize_universal_tx` |
| `revert_universal_tx` | `0x` | not observed |
| `funds_rescued` | `0x` | not observed |

The deployed instruction was renamed to `send_universal_tx`; the registry still carries the
discriminator for the old name. Nothing consumed `identifier` before, so the staleness was
invisible. Anything keyed on it silently matches nothing.

The `event_identifier` values are all correct, confirmed by recomputing the Anchor hashes:
`event:UniversalTx`, `event:UniversalTxFinalized`, `event:RevertUniversalTx`,
`event:FundsRescued`.

Until the registry is fixed the listener logs a warning at startup naming the methods with no
usable instruction discriminator, so the gap is visible instead of looking like coverage.

### Separate anomaly found while verifying this

`finalize_universal_tx` emits the `UniversalTx` event (`6c9ad829b5ea1d7c`) in addition to its
own, in 6 of the ~19 finalize transactions sampled. Example:
`2TnARo9vCAEJswQNBwLQh3yp…`, where a single `FinalizeUniversalTx` instruction emits both
discriminators from the gateway frame at depth 1.

The client maps `6c9ad829b5ea1d7c` to `send_funds`, and the log is genuinely gateway-attributed,
so it passes every check the current code makes and is stored as an inbound. Whether that
produces a spurious inbound depends on downstream deduplication and voting, which has not been
traced. Flagged for the contract team: it is not part of F-2026-18198 and is not addressed here.

### Why truncation cannot be prevented client side

The log budget is per transaction and shared by every program in it, so our own log volume is
not the deciding factor. Measured across 50 devnet gateway transactions, the largest total log
payload was 2192 bytes against a roughly 10 KB budget, and none were truncated. Headroom is
about 4x today.

It is exceeded by composition: a caller placing log-heavy instructions in the same transaction
as `send_universal_tx` pushes our event past the cut. That is outside our control, which is why
recovery (B2) is the durable answer and trimming program logs is not.

### Option C: migrate events to `emit_cpi!`

Anchor's `emit_cpi!` emits an event as a self-CPI, so it arrives in `meta.innerInstructions`
as a real instruction with the program ID attached as structured data. No string parsing, no
log buffer.

- Cost: high. Solana program change plus deploy, then a client change.
- Closes forgery: yes, structurally.
- Closes truncation: yes.
- Requires Solana program change: yes.

Worth doing if the gateway program is being revised anyway. Strictly more expensive than
option B for the same benefit, since option B needs no deploy.

### Option D: cross-check against balance deltas

`meta.preBalances` / `postBalances` and `preTokenBalances` / `postTokenBalances` are always
returned and never truncated. They can corroborate token and amount.

Not sufficient alone. They do not carry recipient, payload, tx type or revert recipient, all
of which exist only in the event. Useful as a defence-in-depth check on an already-observed
event, not as a source of truth. This is part of the auditor's recommendation 6.

## Recommendation

1. Ship the current PR. Forgery is closed and a dropped event is now identifiable rather than
   merely suspected.
2. Fix the registry `identifier` for `send_funds` to `9113a437a5dc1761` and populate the three
   `0x` placeholders. Until then the detection in B1 matches nothing. This is a registry
   change, not a code change, and it is the cheapest item on this list.
3. Obtain the gateway IDL or program source, then implement B2 behind a shadow check. That is
   what actually recovers a truncated deposit instead of reporting it.
4. Have the contract team confirm whether `finalize_universal_tx` emitting `UniversalTx` is
   intended, and whether the client should be ignoring it.
5. Treat option C as the long-term direction only if the Solana program is being revised for
   other reasons.
6. Independently of all of the above, review historical Solana inbounds for forged events. The
   fix prevents new ones but says nothing about whether the vector was used before it shipped.
