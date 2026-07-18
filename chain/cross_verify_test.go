package chain

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/wire"
)

// TestCrossVerify_LBRYDClaimID verifies our claim ID matches what lbcd
// produces for the same input.
//
// Reference (from lbcd/claimtrie/change/claimid.go):
//   id = RIPEMD160(SHA256(txid_internal || vout_be))  (internal byte order)
//   lbcd String() reverses id before hex encoding.
//   txid = 0xab00... (31 zeros), vout = 5
//   internal bytes = 186f0042986695921100febae3f571ab9f369614
//   lbcd display   = 1496369fab71f5e3bafe00119295669842006f18
func TestCrossVerify_LBRYDClaimID(t *testing.T) {
	txidBytes := make([]byte, 32)
	txidBytes[0] = 0xab
	hash, _ := chainhash.NewHash(txidBytes)

	id := NewClaimID(wire.OutPoint{Hash: *hash, Index: 5})

	wantInternalHex := "186f0042986695921100febae3f571ab9f369614"
	wantDisplay := "1496369fab71f5e3bafe00119295669842006f18"
	want, _ := hex.DecodeString(wantInternalHex)
	if len(want) != 20 {
		t.Fatalf("bad reference: %s", wantInternalHex)
	}

	for i := range id {
		if id[i] != want[i] {
			t.Errorf("claim ID internal bytes mismatch at byte %d:\n  got:  %x\n  want: %s",
				i, id[:], wantInternalHex)
			break
		}
	}

	// lbcd reverses bytes before String(); verify our String() matches.
	if id.String() != wantDisplay {
		t.Errorf("claim ID String() mismatch:\n  got:  %s\n  want: %s", id.String(), wantDisplay)
	}

	t.Logf("✓ Claim ID internal bytes match lbcd: %x", id[:])
	t.Logf("✓ Claim ID display matches lbcd: %s", id.String())
}

// TestCrossVerify_TxID verifies our transaction builder produces structurally
// valid transactions that serialize identically to wire.MsgTx.
func TestCrossVerify_TxID(t *testing.T) {
	// Build a transaction identical to the lbcd wire TestTxHash vector
	// (first tx from block 113875) to verify byte-for-byte serialization match.
	msgTx := wire.NewMsgTx(1)
	msgTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{},
			Index: 0xffffffff,
		},
		SignatureScript: []byte{0x04, 0x31, 0xdc, 0x00, 0x1b, 0x01, 0x62},
		Sequence:        0xffffffff,
	})
	msgTx.AddTxOut(&wire.TxOut{
		Value: 5000000000,
		PkScript: []byte{
			0x41, // OP_DATA_65
			0x04, 0xd6, 0x4b, 0xdf, 0xd0, 0x9e, 0xb1, 0xc5,
			0xfe, 0x29, 0x5a, 0xbd, 0xeb, 0x1d, 0xca, 0x42,
			0x81, 0xbe, 0x98, 0x8e, 0x2d, 0xa0, 0xb6, 0xc1,
			0xc6, 0xa5, 0x9d, 0xc2, 0x26, 0xc2, 0x86, 0x24,
			0xe1, 0x81, 0x75, 0xe8, 0x51, 0xc9, 0x6b, 0x97,
			0x3d, 0x81, 0xb0, 0x1c, 0xc3, 0x1f, 0x04, 0x78,
			0x34, 0xbc, 0x06, 0xd6, 0xd6, 0xed, 0xf6, 0x20,
			0xd1, 0x84, 0x24, 0x1a, 0x6a, 0xed, 0x8b, 0x63,
			0xa6, // 65-byte signature
			0xac, // OP_CHECKSIG
		},
	})
	msgTx.LockTime = 0

	// Known vector from lbcd/wire/msgtx_test.go TestTxHash
	want := "f051e59b5e2503ac626d03aaeac8ab7be2d72ba4b7e97119c5852d70d52dcb86"
	txid := TxID(msgTx)
	if txid != want {
		t.Errorf("TxID mismatch:\n  got:  %s\n  want: %s", txid, want)
	} else {
		t.Logf("✓ TxID matches lbcd known vector: %s", txid)
	}

	// Verify the serialized tx round-trips through wire.MsgTx.
	raw, err := Serialize(msgTx)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	t.Logf("Serialized tx (%d bytes): %s", len(raw), hex.EncodeToString(raw))

	tx2 := wire.NewMsgTx(wire.TxVersion)
	if err := tx2.Deserialize(bytes.NewReader(raw)); err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if TxID(tx2) != txid {
		t.Errorf("TxID not preserved through round-trip: %s != %s", TxID(tx2), txid)
	} else {
		t.Logf("✓ Round-trip preserves TxID")
	}

	// Structural checks
	if len(msgTx.TxIn) != 1 {
		t.Errorf("expected 1 input, got %d", len(msgTx.TxIn))
	}
	if len(msgTx.TxOut) != 1 {
		t.Errorf("expected 1 output, got %d", len(msgTx.TxOut))
	}
	if msgTx.Version != 1 {
		t.Errorf("expected version 1, got %d", msgTx.Version)
	}
	_ = fmt.Sprintf
}

// TestCrossVerify_ClaimNameScript verifies our claim_name script bytes match
// lbcd's txscript.ClaimNameScript output (with OP_TRUE stripped, P2PKH appended).
//
// Reference: ClaimNameScript("hello", "value") with OP_TRUE stripped +
//   P2PKH("bU269...") = b50568656c6c6f0576616c75656d7576a914a7b8288564a83ecc34cdd4be7b24ea9ad28062d988ac
func TestCrossVerify_ClaimNameScript(t *testing.T) {
	script, err := BuildClaimNameScript("hello", []byte("value"), "bU269oqn4skRpj6J3uS96wJg6XDyTAfbvz", LBRYParams())
	if err != nil {
		t.Fatalf("BuildClaimNameScript: %v", err)
	}

	want := "b50568656c6c6f0576616c75656d7576a914a7b8288564a83ecc34cdd4be7b24ea9ad28062d988ac"
	got := hex.EncodeToString(script)

	if got != want {
		t.Errorf("claim_name script bytes mismatch:\n  got:  %s\n  want: %s", got, want)
	} else {
		t.Logf("✓ Claim name script matches lbcd: %s", got)
	}
}
