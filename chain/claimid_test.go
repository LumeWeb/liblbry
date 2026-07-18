package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/wire"
	"golang.org/x/crypto/ripemd160"
)

// TestNewClaimID_KnownVector verifies claim ID derivation against a known
// (txid, vout) → claimID vector.
//
// The algorithm is: RIPEMD160(SHA256(txid_bytes || vout_be_uint32))
// where txid_bytes is the raw 32-byte internal-order hash.
func TestNewClaimID_KnownVector(t *testing.T) {
	// Construct a known input: all-zero txid, vout=0
	txidBytes := make([]byte, 32) // all zeros
	hash, _ := chainhash.NewHash(txidBytes)
	op := wire.OutPoint{Hash: *hash, Index: 0}

	id := NewClaimID(op)

	// Independently compute the expected value
	var buf [36]byte
	copy(buf[:32], txidBytes)
	// vout=0 → last 4 bytes stay zero
	sha := sha256.Sum256(buf[:])
	r := ripemd160.New()
	r.Write(sha[:])
	want := r.Sum(nil)

	if id != [20]byte(want) {
		t.Errorf("claim ID mismatch:\n  got:  %x\n  want: %x", id[:], want)
	}
}

func TestNewClaimID_VoutAffectsID(t *testing.T) {
	txidHex := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	hash, _ := chainhash.NewHashFromStr(txidHex)

	id0 := NewClaimID(wire.OutPoint{Hash: *hash, Index: 0})
	id1 := NewClaimID(wire.OutPoint{Hash: *hash, Index: 1})

	if id0 == id1 {
		t.Error("different vouts must produce different claim IDs")
	}
}

func TestNewClaimIDFromTxVout(t *testing.T) {
	txidHex := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	id, err := NewClaimIDFromTxVout(txidHex, 0)
	if err != nil {
		t.Fatalf("NewClaimIDFromTxVout: %v", err)
	}

	// Verify round-trip via String
	s := id.String()
	if len(s) != 40 {
		t.Errorf("claim ID hex must be 40 chars, got %d: %q", len(s), s)
	}

	id2, err := NewClaimIDFromString(s)
	if err != nil {
		t.Fatalf("NewClaimIDFromString: %v", err)
	}
	if id != id2 {
		t.Errorf("round-trip mismatch: %x != %x", id, id2)
	}
}

func TestNewClaimIDFromString_Invalid(t *testing.T) {
	_, err := NewClaimIDFromString("not hex !@#$")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
	_, err = NewClaimIDFromString("abcd") // wrong length
	if err == nil {
		t.Error("expected error for wrong length")
	}
}

func TestClaimID_Equal(t *testing.T) {
	var a, b ClaimID
	a[0] = 1
	b[0] = 1
	if !a.Equal(b) {
		t.Error("Equal: identical IDs should be equal")
	}
	b[0] = 2
	if a.Equal(b) {
		t.Error("Equal: different IDs should not be equal")
	}
}

func TestClaimID_IsZero(t *testing.T) {
	var z ClaimID
	if !z.IsZero() {
		t.Error("zero ClaimID should be IsZero")
	}
	z[5] = 1
	if z.IsZero() {
		t.Error("non-zero ClaimID should not be IsZero")
	}
}

func TestClaimID_Bytes(t *testing.T) {
	id := ClaimID{1, 2, 3}
	b := id.Bytes()
	if len(b) != 20 {
		t.Errorf("Bytes() length = %d, want 20", len(b))
	}
	if b[0] != 1 || b[1] != 2 || b[2] != 3 {
		t.Error("Bytes() content mismatch")
	}
}

func TestClaimID_String(t *testing.T) {
	id := ClaimID{}
	for i := range id {
		id[i] = byte(i)
	}
	s := id.String()
	if len(s) != 40 {
		t.Errorf("String() length = %d, want 40", len(s))
	}
	// lbcd reverses bytes before hex encoding: id[i] becomes s[38-2*i].
	want := "131211100f0e0d0c0b0a09080706050403020100"
	if s != want {
		t.Errorf("String() = %q, want %q", s, want)
	}
}

func TestNewClaimIDFromBytes_Invalid(t *testing.T) {
	_, err := NewClaimIDFromBytes([]byte{1, 2, 3})
	if err == nil {
		t.Error("expected error for wrong length")
	}
	id, err := NewClaimIDFromBytes(make([]byte, 20))
	if err != nil {
		t.Fatalf("NewClaimIDFromBytes(20): %v", err)
	}
	if !id.IsZero() {
		t.Error("expected zero ClaimID")
	}
}

// Cross-verification: our derivation must match lbcd's exact output for the
// same input. We compute independently using sha256+ripemd160 and compare.
func TestNewClaimID_LBCDCompatible(t *testing.T) {
	txidHex := "f0b31d9b9f1a7c8e5d6a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e"
	vout := uint32(7)
	hash, _ := chainhash.NewHashFromStr(txidHex)

	id := NewClaimID(wire.OutPoint{Hash: *hash, Index: vout})

	// Compute manually with big-endian vout
	var buf [36]byte
	copy(buf[:32], hash[:])
	buf[32] = byte(vout >> 24)
	buf[33] = byte(vout >> 16)
	buf[34] = byte(vout >> 8)
	buf[35] = byte(vout)
	sha := sha256.Sum256(buf[:])
	r := ripemd160.New()
	r.Write(sha[:])
	want := r.Sum(nil)

	if !equalBytes(id[:], want) {
		t.Errorf("claim ID mismatch for (txid=%s, vout=%d):\n  got:  %s\n  want: %s",
			txidHex, vout, hex.EncodeToString(id[:]), hex.EncodeToString(want))
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
