// Package chain provides LBRY chain types: addresses, scripts, claim IDs,
// and transaction construction. All operations are pure data — no I/O.
//
// This package imports lbcd subpackages (btcec, chaincfg, chainhash, wire,
// txscript) for consensus-compliant crypto and script building. These are
// lightweight Go packages with no heavy storage dependencies.
package chain

// LBRY chain constants. Mirror lbcd/chaincfg.MainNetParams.

const (
	// P2PKHAddrID is the version byte for LBRY pay-to-pubkey-hash addresses.
	P2PKHAddrID byte = 0x55

	// WIFPrivKeyID is the version byte for wallet import format private keys.
	WIFPrivKeyID byte = 0x1c

	// SLIP44CoinType is the BIP44 coin type for LBRY (unused for derivation,
	// LBRY uses simple m/chain/index instead of BIP44).
	SLIP44CoinType uint32 = 293
)

// Script opcodes used in LBRY claim scripts.
// Source of truth: lbcd/txscript/opcode.go.
const (
	// Standard Bitcoin script opcodes
	OP_0         byte = 0x00
	OP_FALSE     byte = OP_0
	OP_PUSHDATA1 byte = 0x4c
	OP_PUSHDATA2 byte = 0x4d
	OP_PUSHDATA4 byte = 0x4e
	OP_1         byte = 0x51
	OP_TRUE      byte = OP_1
	OP_2         byte = 0x52
	OP_3         byte = 0x53
	OP_DATA_1    byte = 0x01
	OP_DATA_75   byte = 0x4b

	OP_2DROP byte = 0x6d
	OP_DROP  byte = 0x75
	OP_DUP   byte = 0x76

	OP_EQUALVERIFY byte = 0x88
	OP_HASH160     byte = 0xa9
	OP_CHECKSIG    byte = 0xac

	// LBRY-specific opcodes
	OP_CLAIMNAME    byte = 0xb5
	OP_SUPPORTCLAIM byte = 0xb6
	OP_UPDATECLAIM  byte = 0xb7
)

// SigHashType is the signature hash type byte appended to the signature.
type SigHashType byte

const (
	SigHashOld          SigHashType = 0x0
	SigHashAll          SigHashType = 0x1
	SigHashNone         SigHashType = 0x2
	SigHashSingle       SigHashType = 0x3
	SigHashAnyOneCanPay SigHashType = 0x80

	sigHashMask = 0x1f
)
