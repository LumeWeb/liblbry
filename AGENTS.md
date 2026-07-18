# liblbry Agent Context

## Project Overview

liblbry is a comprehensive Go library for the LBRY protocol. It handles both core protocol concerns (wallets, chain types, claims, transactions) and the legacy data layer (blob storage, P2P transfers, DHT, streaming). The codebase is ~31k lines across 15 packages.

## Repository Structure

| Package | Files | Purpose | Status |
|---------|-------|---------|--------|
| `wallet/` | 8 | BIP39/BIP32 HD wallet, address gap scanning | **new** |
| `chain/` | 8 | Claim ID derivation, LBRY scripts, TX builder | **new** |
| `claim/` | 2 | Protobuf claim builders, channel signing | **new** |
| `coinselect/` | 2 | UTXO selection (BnB + best-match) | **new** |
| `electrum/` | 5 | ElectrumX JSON-RPC types and client | **new** |
| `blob/` | 35 | Blob encryption, P2P transfer, DHT discovery | legacy |
| `protocol/` | 52 | DHT, peer protocol, reflector protocol | legacy |
| `server/` | 8 | Server builder with fluent API | legacy |
| `client/` | 8 | Stream/blob acquisition | legacy |
| `storage/` | 15 | BlobStore interface (memory, disk backends) | legacy |
| `stream/` | 23 | SD blob handling, manifests, multihash | legacy |
| `crypto/` | 5 | SHA-384 hashing, encryption helpers | legacy |
| `errors/` | 2 | Custom error types | legacy |
| `internal/` | 3 | Internal utilities | legacy |
| `mocks/` | 4 | Test mocks | legacy |

Root package (`liblbry`): `acquirer.go` (BlobAcquirer interface), `store.go` (StoreFactory, logger options).

## Design Constraints

1. **WASM-compatible**: No C bindings. No heavy deps (Pebble, LevelDB).
2. **pure Go**: Core packages (wallet, chain, claim, coinselect, electrum) are logic-only; no I/O.
3. **Consensus compliance**: `lbcd` is source of truth for claim IDs, scripts, signing digests.
4. **Protobuf**: Claims use `github.com/lbryio/types/v2/go`.
5. **Single-commit PRs**: Squash and force-push.

## New Package Details (Phase 1-2)

### wallet/
- `mnemonic.go` — SHA-512 based seed generation with Electrum wordlist
- `hdkey.go` — BIP32 ExtendedKey using btcec (no lbcutil/hdkeychain)
- `wallet.go` — Wallet struct, `NewFromMnemonic`, address derivation
- `account.go` — AddressManager with configurable gap limit
- `params.go` — LBRY chain constants (P2PKH prefix, WIF prefix, HD versions)

### chain/
- `claimid.go` — `NewClaimID` via RIPEMD160(SHA256(txid || vout)), byte-reversed display strings
- `script.go` — ClaimNameScript, UpdateClaimScript, SupportClaimScript, PayToAddrScript
- `tx.go` — Transaction builder (`Builder`), input/output management, `CalcSignatureHash`
- `params.go` — Chain params re-exported from lbcd
- `cross_verify_test.go` — Byte-level consensus tests against lbcd

### claim/
- `claim.go` — `Helper` struct, `NewStream`, `NewChannel`, `NewRepost`, `NewCollection`, `CompileValue`, `SignStream`, `ParseClaimValue`, `CompactSignature`
- `claim_test.go` — 20 tests including cross-implementation vector with Python/coincurve

### coinselect/
- `coinselect.go` — BnB exact match, closest-match fallback, fee estimation
- Metadata: `Waste`, `ExactMatch`, `Tries` (not in reference impl, added for caller ergonomics)

### electrum/
- `client.go` — Generic `Client` interface `CallTyped`
- `methods.go` — ElectrumX method constants
- `types.go` — Balance, History, ScriptHashNotify structs
- `scripthash.go` — ScriptHash calculation (SHA256 reversed)

## Key Patterns

### Claim Value Format
```
version(1 byte) || claimID(20 bytes) || signature(64 bytes) || protobufPayload
```
Matches `lbry-sdk/lbry/schema/base.py` and `lbry.go/schema/stake/stake.go`.

### Claim ID Derivation
```
claimID = RIPEMD160(SHA256(txid_internal || vout))
```
Uses `binary.BigEndian` for vout. Matches `lbcd/claimtrie/change/claimid.go`.

### Txid Handling
- Display order (RPC/block explorers) → `chainhash.NewHashFromStr` parses to internal bytes.
- Internal bytes used in digests and scripts.
- Never `hex.DecodeString` a txid — it doesn't handle byte reversal.

### Signing Digest
```
digest = SHA-256(internalTxidBytes || claimID || protobufPayload)
```
Uses `crypto/sha256`, not `chainhash.DoubleHashB`.

### Compact Signature
64-byte `r || s`, low-S normalized via `if S > N/2 { S = N - S }`. Matches `btcec.Signature.Serialize()`.

## Dependency Strategy

- `lbcd/btcec`, `lbcd/wire`, `lbcd/chaincfg`, `lbcd/txscript` — direct import.
- `lbryio/lbcutil` — standalone module for address encoding.
- `lbryio/types/v2/go` — protobuf types.
- **No replace directive**: Upstream `lbcd` works without pulling Pebble/LevelDB because only clean subpackages are imported.
- Verify bloat: `go list -deps ./... | grep -i pebble` should return nothing.

## Testing Conventions

1. `btcec.PrivKeyFromBytes` — always use non-zero scalars. `make([]byte, 32)` is scalar 0 (invalid).
2. `chainhash.NewHashFromStr` for display-order txids; never `hex.DecodeString`.
3. Cross-verify against lbcd test vectors where possible (block 113875 txid).
4. Regression tests for every validation/error path.
5. Deterministic cross-implementation vectors: `scalar := make([]byte, 32); scalar[31] = 1` (matches Python `b'\x00'*31 + b'\x01'`).
6. `go test ./...` must pass before any push.

## Legacy Packages (pre-existing)

These packages predate the wallet/chain/claim work. They are I/O-heavy (network, disk) and not WASM-compatible without extra work:

- `blob/` — blob struct with AES encryption, P2P transfer orchestration
- `protocol/` — DHT node, peer/reflector protocol handlers, network crawler
- `server/` — fluent server builder, protocol configuration
- `client/` — stream acquisition with retry, blob acquisition
- `storage/` — BlobStore interface, memory+disk implementations, access control
- `stream/` — SD blob parsing, manifest handling, multihash

## Development Rules

- Single-commit squashed PRs (amend into base, not `--squash`).
- One branch per package; follow dependency order.
- `.hermes/` must be gitignored + untracked — verify with `git ls-files | grep .hermes` before push.
- `go test ./...` must pass before any PR is opened.

## Project-Specific Notes

- Kody sometimes reports bugs that contradict the reference implementations (e.g., claim value byte order). Always verify against `lbry-sdk` or `lbry.go` before changing consensus-critical code.
- `pb.Support` Go bindings may lack the `Comment` field despite the proto definition having it. This is a known limitation of the generated Go package.

## Related Repos (local clones)

- `/opt/data/projects/lbcd` — LBRY Foundation lbcd fork (consensus reference)
- `/opt/data/projects/lbry.go` — lbry.go (Go reference for staking/signing)
- `/opt/data/projects/lbry-sdk` — Python lbry-sdk (Python reference)
- `/opt/data/projects/tracker-demo` — tracker-demo (wallet/chain reference)
