// Package chain provides LBRY chain types: addresses, scripts, claim IDs,
// and transaction construction. All operations are pure data — no I/O.
//
// This package imports lbcd subpackages (btcec, chaincfg, chainhash, wire,
// txscript) for consensus-compliant crypto and script building. These are
// lightweight Go packages with no heavy storage dependencies.
package chain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/wire"
	"golang.org/x/crypto/ripemd160"
)

// ClaimIDSize is the length of a claim ID in bytes (20 = 160 bits).
const ClaimIDSize = 20

// ClaimID represents a LBRY claim's identifier.
// Derived as RIPEMD160(SHA256(txid || vout)).
type ClaimID [ClaimIDSize]byte

// NewClaimID returns the Claim ID for a transaction output.
// Algorithm (per lbcd/claimtrie/change/claimid.go):
//   claimID = RIPEMD160(SHA256(txid || vout))
// where txid is in internal byte order and vout is big-endian uint32.
//
// We reimplement this instead of importing lbcd/claimtrie/change
// because that package transitively pulls in lbcutil. The algorithm
// is 5 lines of trivial code.
func NewClaimID(op wire.OutPoint) ClaimID {
	var buf [chainhash.HashSize + 4]byte
	copy(buf[:], op.Hash[:])
	binary.BigEndian.PutUint32(buf[chainhash.HashSize:], op.Index)
	return hash160Claim(buf[:])
}

// NewClaimIDFromTxVout computes the claim ID for a given txid string and vout.
// txid must be in hex, internal byte order (the standard txid representation).
func NewClaimIDFromTxVout(txidHex string, vout uint32) (ClaimID, error) {
	hash, err := chainhash.NewHashFromStr(txidHex)
	if err != nil {
		return ClaimID{}, fmt.Errorf("parse txid: %w", err)
	}
	return NewClaimID(wire.OutPoint{Hash: *hash, Index: vout}), nil
}

// NewClaimIDFromBytes creates a ClaimID from a 20-byte slice.
func NewClaimIDFromBytes(b []byte) (ClaimID, error) {
	var id ClaimID
	if len(b) != ClaimIDSize {
		return id, fmt.Errorf("invalid claim ID length: got %d, want %d", len(b), ClaimIDSize)
	}
	copy(id[:], b)
	return id, nil
}

// NewClaimIDFromString parses a 40-char hex string into a ClaimID.
// The input is expected in lbcd's display convention (bytes reversed
// compared to internal order), matching ClaimID.String().
func NewClaimIDFromString(s string) (ClaimID, error) {
	if len(s) != 40 {
		return ClaimID{}, fmt.Errorf("invalid claim ID hex length: got %d, want 40", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return ClaimID{}, fmt.Errorf("decode claim ID hex: %w", err)
	}
	var id ClaimID
	// Reverse to internal byte order (lbcd NewIDFromString does the same).
	for i, j := 0, ClaimIDSize-1; i < j; i, j = i+1, j-1 {
		id[i], id[j] = b[j], b[i]
	}
	return id, nil
}

// String returns the claim ID as a 40-char hex string in lbcd's display
// convention: internal byte order is reversed before hex encoding.
// This matches lbcd/claimtrie/change/claimid.go ClaimID.String().
func (id ClaimID) String() string {
	var reversed [ClaimIDSize]byte
	for i, j := 0, ClaimIDSize-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = id[j], id[i]
	}
	return hex.EncodeToString(reversed[:])
}

// Bytes returns the claim ID in internal byte order.
func (id ClaimID) Bytes() []byte {
	return id[:]
}

// Equal compares two claim IDs.
func (id ClaimID) Equal(other ClaimID) bool {
	return id == other
}

// IsZero returns true if the claim ID is all zeros.
func (id ClaimID) IsZero() bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

// hash160Claim computes RIPEMD160(SHA256(data)).
func hash160Claim(data []byte) ClaimID {
	sha := sha256.Sum256(data)
	r := ripemd160.New()
	r.Write(sha[:])
	var id ClaimID
	copy(id[:], r.Sum(nil))
	return id
}
