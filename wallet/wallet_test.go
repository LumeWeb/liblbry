package wallet

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestNewFromMnemonic_Valid(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	addr := w.AddressString()
	if addr == "" {
		t.Fatal("address is empty")
	}

	// LBRY mainnet addresses start with 'b'.
	if addr[0] != 'b' {
		t.Errorf("address must start with 'b', got %q", addr)
	}
	t.Logf("address: %s", addr)
}

func TestNewFromMnemonic_Invalid(t *testing.T) {
	_, err := NewFromMnemonic("not a valid mnemonic phrase at all")
	if err == nil {
		t.Fatal("expected error for invalid mnemonic")
	}
}

func TestNewFromMnemonic_Deterministic(t *testing.T) {
	w1, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("w1: %v", err)
	}

	w2, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("w2: %v", err)
	}

	if w1.AddressString() != w2.AddressString() {
		t.Errorf("addresses differ: %s vs %s", w1.AddressString(), w2.AddressString())
	}
}

func TestPrivateKeyHex(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	privHex := w.PrivateKeyHex()
	if len(privHex) != 64 {
		t.Errorf("private key hex must be 64 chars, got %d", len(privHex))
	}

	if _, err := hex.DecodeString(privHex); err != nil {
		t.Errorf("invalid private key hex: %v", err)
	}
}

func TestPublicKeyHex(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	pubHex := w.PublicKeyHex()
	if len(pubHex) != 66 {
		t.Errorf("compressed public key hex must be 66 chars, got %d", len(pubHex))
	}
}

func TestDeriveKey_DifferentAddresses(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	addr0, err := w.AddressStringAt(0, 0)
	if err != nil {
		t.Fatalf("AddressStringAt(0,0): %v", err)
	}

	addr1, err := w.AddressStringAt(0, 1)
	if err != nil {
		t.Fatalf("AddressStringAt(0,1): %v", err)
	}

	if addr0 == addr1 {
		t.Error("addresses at different indices should differ")
	}

	if addr0[0] != 'b' {
		t.Errorf("LBRY mainnet address must start with 'b', got %q", addr0)
	}
}

// TestDeriveKey_LBRYPath verifies the wallet uses m/0/0 derivation
// (not BIP44 m/44'/293'/0'/0/0). The LBRY chain does NOT use BIP44.
func TestDeriveKey_LBRYPath(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	// m/0/0 is the default address (set in deriveAccount0).
	addr0 := w.AddressString()
	if addr0 == "" {
		t.Fatal("default address (m/0/0) is empty")
	}
	if addr0[0] != 'b' {
		t.Errorf("LBRY mainnet address must start with 'b', got %q", addr0)
	}

	// DeriveKey(0, 0) should produce the same address as the default.
	addr00, err := w.AddressStringAt(0, 0)
	if err != nil {
		t.Fatalf("AddressStringAt(0,0): %v", err)
	}
	if addr00 != addr0 {
		t.Errorf("AddressStringAt(0,0) = %s, want default address %s (m/0/0 mismatch)", addr00, addr0)
	}

	// DeriveKey(0, 1) should produce a DIFFERENT address.
	addr01, err := w.AddressStringAt(0, 1)
	if err != nil {
		t.Fatalf("AddressStringAt(0,1): %v", err)
	}
	if addr01 == addr0 {
		t.Error("AddressStringAt(0,1) produced same address as m/0/0 — derivation path is wrong")
	}
}

// TestDeriveKey_ChangeChain verifies that chain 1 (change) produces
// different addresses than chain 0 (receiving).
func TestDeriveKey_ChangeChain(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	receiving, err := w.AddressStringAt(0, 0)
	if err != nil {
		t.Fatalf("receiving: %v", err)
	}

	change, err := w.AddressStringAt(1, 0)
	if err != nil {
		t.Fatalf("change: %v", err)
	}

	if receiving == change {
		t.Error("receiving and change addresses should differ at same index")
	}
}

func TestExtendedKey_String(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	xprv := w.MasterKey().String()
	if !strings.HasPrefix(xprv, "xprv") {
		t.Errorf("master key should start with 'xprv', got %q", xprv[:10])
	}
}

func TestAddress_PubKeyScript(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	script := w.Address().PubKeyScript()
	// P2PKH script: OP_DUP OP_HASH160 <push 20> <20 bytes> OP_EQUALVERIFY OP_CHECKSIG = 25 bytes
	if len(script) != 25 {
		t.Errorf("P2PKH script must be 25 bytes, got %d", len(script))
	}
	if script[0] != 0x76 {
		t.Errorf("first byte must be OP_DUP (0x76), got 0x%02x", script[0])
	}
	if script[len(script)-1] != 0xac {
		t.Errorf("last byte must be OP_CHECKSIG (0xac), got 0x%02x", script[len(script)-1])
	}
}

func TestAddressManager_GapLimit(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	am := NewAddressManager(w, ChainReceiving, 5)
	newAddrs, err := am.EnsureGap()
	if err != nil {
		t.Fatalf("EnsureGap: %v", err)
	}
	// No addresses used yet → derive gapLimit=5 addresses (indices 0..4).
	if len(newAddrs) != 5 {
		t.Errorf("expected 5 initial addresses, got %d", len(newAddrs))
	}

	// Mark index 2 as used. After this, we need indices 0..7 (8 total)
	// so that indices 3..7 are the 5 unused addresses beyond lastUsed.
	moreAddrs, err := am.MarkUsed(2)
	if err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	if len(moreAddrs) != 3 {
		t.Errorf("expected 3 new addresses after MarkUsed(2) (5,6,7), got %d", len(moreAddrs))
	}

	if am.LastUsedIndex() != 2 {
		t.Errorf("LastUsedIndex = %d, want 2", am.LastUsedIndex())
	}

	// Total addresses should be 8 (indices 0-7).
	total := len(am.CurrentAddresses())
	if total != 8 {
		t.Errorf("expected 8 total addresses, got %d", total)
	}

	// Verify that addresses 3..7 are marked unused.
	for _, a := range am.CurrentAddresses() {
		if a.Index > 2 && a.Used {
			t.Errorf("address at index %d should be unused", a.Index)
		}
		if a.Index == 2 && !a.Used {
			t.Error("address at index 2 should be marked used")
		}
	}
}

func TestAddressManager_Unused(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	am := NewAddressManager(w, ChainReceiving, 3)
	_, err = am.EnsureGap()
	if err != nil {
		t.Fatalf("EnsureGap: %v", err)
	}

	// Mark index 0 used. Now we need 0..3 (4 total, 3 unused at 1,2,3).
	_, err = am.MarkUsed(0)
	if err != nil {
		t.Fatalf("MarkUsed(0): %v", err)
	}

	unused := am.UnusedAddresses()
	if len(unused) != 3 {
		t.Errorf("expected 3 unused (indices 1-3), got %d", len(unused))
	}

	next, err := am.NextUnusedAddress()
	if err != nil {
		t.Fatalf("NextUnusedAddress: %v", err)
	}
	if next.Index != 1 {
		t.Errorf("next unused index = %d, want 1", next.Index)
	}
}
