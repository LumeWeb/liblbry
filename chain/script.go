package chain

import (
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
