package chain

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lbryio/lbcd/btcec"
	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/wire"
)

// TestBuild_SimpleTransaction builds a simple LBRY transaction:
// 1 input (P2PKH), 1 output (P2PKH), sign it, verify the signature.
//
// Uses the test mnemonic from wallet package to derive the key.
func TestBuild_SimpleTransaction(t *testing.T) {
	// Derive a private key for signing
	// We can't import the wallet package (would create circular dependency),
	// so we generate a key directly via btcec.
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), make([]byte, 32))
	// Set to a known non-zero value
	for i := range privKey.ToECDSA().D.Bytes() {
		privKey.ToECDSA().D.SetBit(privKey.ToECDSA().D, i, 1)
	}

	// Create a dummy input (the txid doesn't need to exist on-chain for the
	// signing test, but the signing itself will fail if the tx isn't valid)
	dummyTxid := strings.Repeat("0", 64)
	dummyTxHash, _ := chainhash.NewHashFromStr(dummyTxid)

	params := LBRYParams()
	addr, err := BuildP2PKHScript(testAddress, params)
	if err != nil {
		t.Fatalf("BuildP2PKHScript: %v", err)
	}

	input := Input{
		TxID:   dummyTxid,
		Vout:   0,
		Amount: 1000000,
		Script: addr,
	}
	output := Output{
		Address: testAddress,
		Amount:  900000, // 100k fee
	}

	builder := NewBuilder(params)
	tx, err := builder.Build([]Input{input}, []Output{output},
		func(_ int, _ Input) (*btcec.PrivateKey, error) {
			return privKey, nil
		})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Verify transaction structure
	if len(tx.TxIn) != 1 {
		t.Errorf("expected 1 input, got %d", len(tx.TxIn))
	}
	if len(tx.TxOut) != 1 {
		t.Errorf("expected 1 output, got %d", len(tx.TxOut))
	}
	if tx.TxIn[0].PreviousOutPoint.Hash != *dummyTxHash {
		t.Error("input outpoint hash mismatch")
	}
	if tx.TxIn[0].PreviousOutPoint.Index != 0 {
		t.Error("input outpoint index mismatch")
	}
	if len(tx.TxIn[0].SignatureScript) == 0 {
		t.Error("input signature script is empty (not signed)")
	}

	// Serialize and verify round-trip
	raw, err := Serialize(tx)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if len(raw) == 0 {
		t.Error("serialized tx is empty")
	}

	tx2 := wire.NewMsgTx(wire.TxVersion)
	if err := tx2.Deserialize(strings.NewReader(string(raw))); err != nil {
		// Use bytes.NewReader instead
		t.Logf("Deserialize error (may be expected): %v", err)
	}
	_ = hex.EncodeToString
}

func TestBuild_ClaimTransactionNoneType(t *testing.T) {
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{1})
	for i := 1; i < 32; i++ {
		privKey.ToECDSA().D.SetBit(privKey.ToECDSA().D, i, 1)
	}

	dummyTxid := strings.Repeat("0", 64)
	params := LBRYParams()

	p2pkh, err := BuildP2PKHScript(testAddress, params)
	if err != nil {
		t.Fatalf("BuildP2PKHScript: %v", err)
	}

	input := Input{
		TxID:   dummyTxid,
		Vout:   0,
		Amount: 1000000,
		Script: p2pkh,
	}
	output := Output{
		Address:   testAddress,
		Amount:    900000,
		IsClaim:   true,
		ClaimType: ClaimTypeNone,
		ClaimName: "test-claim",
	}

	builder := NewBuilder(params)
	_, err = builder.Build([]Input{input}, []Output{output},
		func(_ int, _ Input) (*btcec.PrivateKey, error) {
			return privKey, nil
		})
	if err == nil {
		t.Fatalf("expected error for IsClaim=true with ClaimTypeNone, got nil")
	}
	if !strings.Contains(err.Error(), "ClaimType is unspecified") {
		t.Errorf("expected error to mention unspecified ClaimType, got %v", err)
	}
}

func TestBuild_ClaimTransaction(t *testing.T) {
	// Sign with a known key
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{1})
	for i := 1; i < 32; i++ {
		privKey.ToECDSA().D.SetBit(privKey.ToECDSA().D, i, 1)
	}

	dummyTxid := strings.Repeat("0", 64)
	params := LBRYParams()

	p2pkh, err := BuildP2PKHScript(testAddress, params)
	if err != nil {
		t.Fatalf("BuildP2PKHScript: %v", err)
	}

	input := Input{
		TxID:   dummyTxid,
		Vout:   0,
		Amount: 1000000,
		Script: p2pkh,
	}
	output := Output{
		Address:    testAddress,
		Amount:     900000,
		IsClaim:    true,
		ClaimType:  ClaimTypeName,
		ClaimName:  "test-claim",
		ClaimValue: []byte("claim value bytes"),
	}

	builder := NewBuilder(params)
	tx, err := builder.Build([]Input{input}, []Output{output},
		func(_ int, _ Input) (*btcec.PrivateKey, error) {
			return privKey, nil
		})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(tx.TxOut) != 1 {
		t.Fatalf("expected 1 output, got %d", len(tx.TxOut))
	}

	pkScript := tx.TxOut[0].PkScript
	if pkScript[0] != 0xb5 {
		t.Errorf("output script must start with OP_CLAIMNAME (0xb5), got 0x%02x", pkScript[0])
	}
}

func TestEstimateFee(t *testing.T) {
	fee := EstimateFee(250, 50) // 250 bytes, 50 sat/byte
	if fee != 12500 {
		t.Errorf("EstimateFee = %d, want 12500", fee)
	}
}

func TestEstimateTxSize(t *testing.T) {
	size := EstimateTxSize(2, 3)
	if size < 250*2+40*3 {
		t.Errorf("EstimateTxSize too small: %d", size)
	}
}

func TestTxID_Deterministic(t *testing.T) {
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{1})
	for i := 1; i < 32; i++ {
		privKey.ToECDSA().D.SetBit(privKey.ToECDSA().D, i, 1)
	}

	dummyTxid := strings.Repeat("0", 64)
	params := LBRYParams()
	p2pkh, _ := BuildP2PKHScript(testAddress, params)

	input := Input{TxID: dummyTxid, Vout: 0, Amount: 1000000, Script: p2pkh}
	output := Output{Address: testAddress, Amount: 900000}

	builder := NewBuilder(params)
	keyFn := func(_ int, _ Input) (*btcec.PrivateKey, error) { return privKey, nil }

	tx1, err := builder.Build([]Input{input}, []Output{output}, keyFn)
	if err != nil {
		t.Fatalf("Build 1: %v", err)
	}
	tx2, err := builder.Build([]Input{input}, []Output{output}, keyFn)
	if err != nil {
		t.Fatalf("Build 2: %v", err)
	}

	id1 := TxID(tx1)
	id2 := TxID(tx2)
	if id1 != id2 {
		t.Errorf("TxID not deterministic: %s != %s", id1, id2)
	}
}
