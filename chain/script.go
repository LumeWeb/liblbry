package chain

import (
	"encoding/binary"
	"fmt"

	"github.com/lbryio/lbcd/btcec"
	"github.com/lbryio/lbcd/chaincfg"
	"github.com/lbryio/lbcd/txscript"
	"github.com/lbryio/lbcd/wire"
	"github.com/lbryio/lbcutil"
)

// Script type identifiers for claim operations.
// These are convenience constants matching lbcd/txscript.OP_* values.
const (
	ScriptTypeClaimName    = "claim_name"
	ScriptTypeUpdateClaim  = "update_claim"
	ScriptTypeSupportClaim = "support_claim"
)

// BuildClaimNameScript constructs the output script for a claim_name output.
//
// The script is:
//   OP_CLAIMNAME <name> <value> OP_2DROP OP_DROP OP_TRUE
// appended with a P2PKH template (OP_DUP OP_HASH160 <20> OP_EQUALVERIFY OP_CHECKSIG).
//
// The OP_TRUE placeholder is stripped because the P2PKH template replaces it.
func BuildClaimNameScript(name string, value []byte, address string, params *chaincfg.Params) ([]byte, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("claim name cannot be empty")
	}
	if len(name) > MaxClaimNameSize {
		return nil, fmt.Errorf("claim name too long: %d > %d", len(name), MaxClaimNameSize)
	}

	addr, err := lbcutil.DecodeAddress(address, params)
	if err != nil {
		return nil, fmt.Errorf("decode address %q: %w", address, err)
	}

	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, fmt.Errorf("build P2PKH script: %w", err)
	}

	prefix, err := txscript.ClaimNameScript(name, string(value))
	if err != nil {
		return nil, fmt.Errorf("build claim name prefix: %w", err)
	}
	// Strip trailing OP_TRUE; P2PKH replaces it.
	if len(prefix) > 0 && prefix[len(prefix)-1] == txscript.OP_TRUE {
		prefix = prefix[:len(prefix)-1]
	}

	return append(prefix, pkScript...), nil
}

// BuildUpdateClaimScript constructs the output script for an update_claim output.
//
// The script is:
//   OP_UPDATECLAIM <name> <claimID> <value> OP_2DROP OP_2DROP OP_TRUE
// appended with P2PKH.
func BuildUpdateClaimScript(name string, claimID ClaimID, value []byte, address string, params *chaincfg.Params) ([]byte, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("claim name cannot be empty")
	}
	if len(name) > MaxClaimNameSize {
		return nil, fmt.Errorf("claim name too long: %d > %d", len(name), MaxClaimNameSize)
	}

	addr, err := lbcutil.DecodeAddress(address, params)
	if err != nil {
		return nil, fmt.Errorf("decode address %q: %w", address, err)
	}

	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, fmt.Errorf("build P2PKH script: %w", err)
	}

	prefix, err := txscript.ClaimUpdateScript(name, claimID[:], string(value))
	if err != nil {
		return nil, fmt.Errorf("build update claim prefix: %w", err)
	}
	// Strip trailing OP_TRUE; P2PKH replaces it.
	if len(prefix) > 0 && prefix[len(prefix)-1] == txscript.OP_TRUE {
		prefix = prefix[:len(prefix)-1]
	}

	return append(prefix, pkScript...), nil
}

// BuildSupportClaimScript constructs the output script for a support_claim output.
//
// The script is:
//   OP_SUPPORTCLAIM <name> <claimID> OP_2DROP OP_DROP OP_TRUE
// (no value) or:
//   OP_SUPPORTCLAIM <name> <claimID> <value> OP_2DROP OP_2DROP OP_TRUE
// appended with P2PKH.
func BuildSupportClaimScript(name string, claimID ClaimID, value []byte, address string, params *chaincfg.Params) ([]byte, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("claim name cannot be empty")
	}
	if len(name) > MaxClaimNameSize {
		return nil, fmt.Errorf("claim name too long: %d > %d", len(name), MaxClaimNameSize)
	}

	addr, err := lbcutil.DecodeAddress(address, params)
	if err != nil {
		return nil, fmt.Errorf("decode address %q: %w", address, err)
	}

	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, fmt.Errorf("build P2PKH script: %w", err)
	}

	prefix, err := txscript.ClaimSupportScript(name, claimID[:], value)
	if err != nil {
		return nil, fmt.Errorf("build support claim prefix: %w", err)
	}
	// Strip trailing OP_TRUE; P2PKH replaces it.
	if len(prefix) > 0 && prefix[len(prefix)-1] == txscript.OP_TRUE {
		prefix = prefix[:len(prefix)-1]
	}

	return append(prefix, pkScript...), nil
}

// BuildP2PKHScript returns the scriptPubKey for a P2PKH output.
// Equivalent to txscript.PayToAddrScript but takes the raw address string.
func BuildP2PKHScript(address string, params *chaincfg.Params) ([]byte, error) {
	addr, err := lbcutil.DecodeAddress(address, params)
	if err != nil {
		return nil, fmt.Errorf("decode address %q: %w", address, err)
	}
	return txscript.PayToAddrScript(addr)
}

// MaxClaimScriptSize limits the size of a claim script (in bytes, not
// including the trailing P2PKH).
const MaxClaimScriptSize = txscript.MaxClaimScriptSize

// MaxClaimNameSize limits the size of a claim name in bytes.
const MaxClaimNameSize = txscript.MaxClaimNameSize

// SignatureScript produces a signature script for signing an input.
// subscript is the P2PKH scriptPubKey of the previous output (with any
// claim prefix already stripped via txscript.StripClaimScriptPrefix).
func SignatureScript(tx *wire.MsgTx, inputIndex int, subscript []byte, hashType SigHashType, privKey *btcec.PrivateKey) ([]byte, error) {
	return txscript.SignatureScript(tx, inputIndex, subscript, txscript.SigHashType(hashType), privKey, true)
}

// CalcSignatureHash computes the signature hash for a transaction input.
// Used by signature verification and for advanced signing flows.
func CalcSignatureHash(subscript []byte, hashType SigHashType, tx *wire.MsgTx, inputIndex int) ([]byte, error) {
	return txscript.CalcSignatureHash(subscript, txscript.SigHashType(hashType), tx, inputIndex)
}

// StripClaimScriptPrefix removes the claim operation prefix from a script,
// returning only the P2PKH template. Used to extract the underlying
// signature script template before signing.
func StripClaimScriptPrefix(script []byte) []byte {
	return txscript.StripClaimScriptPrefix(script)
}

// IsClaimScript returns true if the script starts with a claim opcode.
func IsClaimScript(script []byte) bool {
	return len(script) > 0 && (script[0] == txscript.OP_CLAIMNAME ||
		script[0] == txscript.OP_UPDATECLAIM ||
		script[0] == txscript.OP_SUPPORTCLAIM)
}

// LBRYParams returns the LBRY mainnet chaincfg.Params.
func LBRYParams() *chaincfg.Params {
	p := chaincfg.MainNetParams
	p.HDCoinType = SLIP44CoinType
	return &p
}

// ExtractClaimValue extracts the embedded claim value from a claim script.
// Supports OP_CLAIMNAME (value at push index 1) and OP_UPDATECLAIM (value at push index 2).
//
// This function manually decodes the script to handle txscript.AddData's
// optimization of single-byte values 0x01-0x10 into OP_1..OP_16 opcodes,
// which causes both txscript.ExtractClaimScript and txscript.PushedData
// to lose the data.
func ExtractClaimValue(script []byte) ([]byte, error) {
	if len(script) == 0 {
		return nil, fmt.Errorf("empty script")
	}

	var valueIndex int
	switch script[0] {
	case txscript.OP_CLAIMNAME:
		valueIndex = 1 // pushes: name, value
	case txscript.OP_UPDATECLAIM:
		valueIndex = 2 // pushes: name, claimID, value
	default:
		return nil, fmt.Errorf("unexpected opcode 0x%02x in claim script", script[0])
	}

	pushes, err := readPushes(script[1:])
	if err != nil {
		return nil, fmt.Errorf("parse script pushes: %w", err)
	}
	if len(pushes) <= valueIndex {
		return nil, fmt.Errorf("expected at least %d pushes in claim script, got %d", valueIndex+1, len(pushes))
	}
	return pushes[valueIndex], nil
}

// readPushes scans a byte slice and returns all data pushes, including
// small integer opcodes (OP_1..OP_16) which txscript.AddData optimizes
// into single-byte opcodes.
func readPushes(data []byte) ([][]byte, error) {
	var pushes [][]byte
	i := 0
	for i < len(data) {
		op := data[i]
		i++

		switch {
		// OP_0 pushes empty slice
		case op == txscript.OP_0:
			pushes = append(pushes, []byte{})

		// OP_1..OP_16 push values 1-16 as single-byte data
		case op >= txscript.OP_1 && op <= txscript.OP_16:
			pushes = append(pushes, []byte{op - (txscript.OP_1 - 1)})

		// OP_1NEGATE pushes -1 (0x81)
		case op == txscript.OP_1NEGATE:
			pushes = append(pushes, []byte{0x81})

		// OP_DATA_1 .. OP_DATA_75: next N bytes are the data
		case op >= txscript.OP_DATA_1 && op <= txscript.OP_DATA_75:
			n := int(op - (txscript.OP_DATA_1 - 1))
			if i+n > len(data) {
				return nil, fmt.Errorf("push %d exceeds script length", n)
			}
			push := make([]byte, n)
			copy(push, data[i:i+n])
			pushes = append(pushes, push)
			i += n

		// OP_PUSHDATA1: next 1 byte is length
		case op == txscript.OP_PUSHDATA1:
			if i >= len(data) {
				return nil, fmt.Errorf("truncated OP_PUSHDATA1")
			}
			n := int(data[i])
			i++
			if i+n > len(data) {
				return nil, fmt.Errorf("OP_PUSHDATA1 length %d exceeds script", n)
			}
			push := make([]byte, n)
			copy(push, data[i:i+n])
			pushes = append(pushes, push)
			i += n

		// OP_PUSHDATA2: next 2 bytes (little-endian) are the length
		case op == txscript.OP_PUSHDATA2:
			if i+2 > len(data) {
				return nil, fmt.Errorf("truncated OP_PUSHDATA2")
			}
			n := int(binary.LittleEndian.Uint16(data[i : i+2]))
			i += 2
			if i+n > len(data) {
				return nil, fmt.Errorf("OP_PUSHDATA2 length %d exceeds script", n)
			}
			push := make([]byte, n)
			copy(push, data[i:i+n])
			pushes = append(pushes, push)
			i += n

		// OP_PUSHDATA4: next 4 bytes (little-endian) are the length
		case op == txscript.OP_PUSHDATA4:
			if i+4 > len(data) {
				return nil, fmt.Errorf("truncated OP_PUSHDATA4")
			}
			n := int(binary.LittleEndian.Uint32(data[i : i+4]))
			i += 4
			if n < 0 || n > len(data)-i {
				return nil, fmt.Errorf("OP_PUSHDATA4 length %d exceeds script", n)
			}
			push := make([]byte, n)
			copy(push, data[i:i+n])
			pushes = append(pushes, push)
			i += n

		// Not a push opcode — stop scanning (we hit the suffix like OP_2DROP etc.)
		default:
			i-- // put back the non-push byte, caller can skip suffix
			return pushes, nil
		}
	}
	return pushes, nil
}
