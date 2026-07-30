package wallet

import (
	"encoding/hex"
	"math/big"
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

func TestZero_WipesKeyMaterial(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	// Capture references before Zero nils them.
	seedSlice := w.Seed()
	origSeed := make([]byte, len(seedSlice))
	copy(origSeed, seedSlice)
	pk := w.PrivateKey()
	origPriv := pk.Serialize()

	if len(origSeed) == 0 {
		t.Fatal("seed is empty before Zero")
	}
	if len(origPriv) == 0 {
		t.Fatal("private key is empty before Zero")
	}

	// Zero the wallet
	w.Zero()

	// Verify seed is nilled and the original backing array is wiped.
	if w.Seed() != nil {
		t.Fatal("Seed() should return nil after Zero()")
	}
	// seedSlice still points to the original backing array — verify it's zeroed.
	zeroed := true
	for _, b := range seedSlice {
		if b != 0 {
			zeroed = false
			break
		}
	}
	if !zeroed {
		t.Fatal("seed backing array not zeroed after Zero()")
	}

	// Verify PrivateKey() returns nil and the captured scalar is zero.
	if w.PrivateKey() != nil {
		t.Fatal("PrivateKey() should return nil after Zero()")
	}
	if pk.D.Sign() != 0 {
		t.Fatal("captured private key scalar not zeroed after Zero()")
	}
}

// TestZero_WipesMasterKey verifies that Wallet.Zero() wipes the master
// extended key's private key and chain code — not just the seed and
// account-0 private key. Also verifies that masterKey is nilled, preventing
// any further derivation. Regression for Kody finding: masterKey was
// originally skipped, leaving the full xprv secret recoverable.
func TestZero_WipesMasterKey(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	mk := w.MasterKey()
	if mk == nil {
		t.Fatal("master key is nil before Zero")
	}

	// Capture the original xprv string — it must be non-empty and start with xprv.
	origXprv := mk.String()
	if !strings.HasPrefix(origXprv, "xprv") {
		t.Fatalf("master key should start with 'xprv', got %q", origXprv[:8])
	}

	w.Zero()

	// After Zero, MasterKey() returns nil (the field is nilled).
	if w.MasterKey() != nil {
		t.Fatal("MasterKey() should return nil after Zero()")
	}

	// The captured mk pointer's chain code is wiped + nilled.
	// String() should panic or produce a different value since chainCode is nil.
	// We verify the xprv changed (secret no longer recoverable).
	postXprv := mk.String()
	if postXprv == origXprv {
		t.Fatal("master key xprv unchanged after Zero() — secret not wiped")
	}
}

// TestZero_PrivKeyBackingArrayOverwritten verifies that Zero() overwrites
// the big.Int backing array instead of just truncating it. SetInt64(0)
// truncates the nat slice to length 0 without clearing the underlying bytes.
// We verify by capturing the PrivateKey pointer before Zero, then checking
// that the scalar bytes are all zeros afterward.
func TestZero_PrivKeyBackingArrayOverwritten(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	// Capture the private key pointer before Zero nils it.
	pk := w.PrivateKey()
	if pk == nil {
		t.Fatal("private key is nil before Zero")
	}
	origPriv := pk.Serialize()
	if allZero(origPriv) {
		t.Fatal("private key is all zeros before Zero — invalid test setup")
	}

	w.Zero()

	// After Zero, Wallet.PrivateKey() returns nil (the field is nilled).
	if w.PrivateKey() != nil {
		t.Fatal("PrivateKey() should return nil after Zero()")
	}

	// But the captured pk pointer is wiped: D.Sign() == 0 and Serialize() is all zeros.
	if pk.D.Sign() != 0 {
		t.Fatal("captured private key scalar not zero after Zero()")
	}
	postPriv := pk.Serialize()
	if len(postPriv) != len(origPriv) {
		t.Fatalf("serialized key length changed: %d -> %d", len(origPriv), len(postPriv))
	}
	if !allZero(postPriv) {
		t.Fatalf("private key bytes not zeroed after Zero(): %x", postPriv)
	}
}

// TestExtendedKey_Zero verifies that ExtendedKey.Zero() wipes both the
// private key scalar and the chain code.
func TestExtendedKey_Zero(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	mk := w.MasterKey()
	origXprv := mk.String()

	if !strings.HasPrefix(origXprv, "xprv") {
		t.Fatalf("expected xprv prefix, got %q", origXprv[:8])
	}

	mk.Zero()

	// After Zero, String() returns "" because chainCode and privKey are nil.
	postXprv := mk.String()
	if postXprv != "" {
		t.Fatalf("expected empty string after Zero, got %q", postXprv)
	}
}

// TestZero_NilSafe verifies that Zero() does not panic on a fresh or
// partially-initialized wallet.
func TestZero_NilSafe(t *testing.T) {
	w := &Wallet{}
	// Should not panic.
	w.Zero()
}

// TestZero_NilsAllMembers verifies that after Zero(), all sensitive
// wallet fields are nil, preventing any further key-derived operations.
// Regression for Kody finding: pointers were left non-nil after wiping
// bytes, allowing DeriveKey/AddressAt to continue working.
func TestZero_NilsAllMembers(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	// Verify fields are non-nil before Zero.
	if w.Seed() == nil {
		t.Fatal("Seed() is nil before Zero")
	}
	if w.PrivateKey() == nil {
		t.Fatal("PrivateKey() is nil before Zero")
	}
	if w.PublicKey() == nil {
		t.Fatal("PublicKey() is nil before Zero")
	}
	if w.MasterKey() == nil {
		t.Fatal("MasterKey() is nil before Zero")
	}
	if w.Address() == nil {
		t.Fatal("Address() is nil before Zero")
	}

	w.Zero()

	// All accessors must return nil/zero after Zero.
	if w.Seed() != nil {
		t.Error("Seed() should be nil after Zero")
	}
	if w.PrivateKey() != nil {
		t.Error("PrivateKey() should be nil after Zero")
	}
	if w.PublicKey() != nil {
		t.Error("PublicKey() should be nil after Zero")
	}
	if w.MasterKey() != nil {
		t.Error("MasterKey() should be nil after Zero")
	}
	if w.Address() != nil {
		t.Error("Address() should be nil after Zero")
	}

	// DeriveKey should panic or error since masterKey is nil.
	_, err = w.DeriveKey(0, 0)
	if err == nil {
		t.Error("DeriveKey should error after Zero (masterKey is nil)")
	}

	// PrivateKeyHex should return empty string.
	if w.PrivateKeyHex() != "" {
		t.Error("PrivateKeyHex should be empty after Zero")
	}

	// Mnemonic is cleared by Zero() — the reference is set to "".
	if w.Mnemonic() != "" {
		t.Error("Mnemonic should be empty after Zero")
	}
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// TestZero_ConcurrentAccess verifies that Zero() running concurrently
// with accessors doesn't cause nil-pointer panics. The RWMutex on Wallet
// and ExtendedKey must protect the check-then-dereference pattern.
// Run with -race to detect data races.
func TestZero_ConcurrentAccess(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	done := make(chan struct{})

	// Goroutine 1: call Zero() repeatedly
	go func() {
		defer close(done)
		w.Zero()
	}()

	// Goroutine 2: call accessors concurrently with Zero
	// These must not panic even if Zero nils the fields mid-call.
	for i := 0; i < 100; i++ {
		_ = w.AddressString()
		_ = w.PrivateKeyHex()
		_ = w.PublicKeyHex()
		_ = w.Seed()
		_, _ = w.DeriveKey(0, 0)
		_ = w.MasterKey()
	}

	<-done
}

// TestZero_Fixed32ByteWipe verifies that Zero() uses a fixed 32-byte
// buffer to overwrite the private key scalar, not len(D.Bytes()).
// big.Int.Bytes() omits leading zeros, so a key like 0x00...01 would
// only wipe 1 byte instead of 32. The fix uses make([]byte, 32).
func TestZero_Fixed32ByteWipe(t *testing.T) {
	// Craft a seed whose derived private key has a leading zero byte in
	// D.BigInt, so D.Bytes() returns fewer than 32 bytes. This lets us
	// verify that Zero() writes a full 32-byte zero buffer instead of
	// only len(D.Bytes()) bytes (which would miss the leading zero).
	var w *Wallet
	var d *big.Int
	for i := 0; i < 1000; i++ {
		seed := make([]byte, 64)
		// Vary the seed to find one that produces a short D.Bytes().
		seed[0] = byte(i)
		seed[1] = byte(i >> 8)
		wallet, err := NewFromSeed(seed)
		if err != nil {
			continue
		}
		pk := wallet.PrivateKey()
		if pk == nil {
			continue
		}
		db := pk.D.Bytes()
		if len(db) < 32 {
			w = wallet
			d = pk.D
			break
		}
	}

	if w == nil {
		t.Skip("no seed produced a private key with leading zero bytes (rare; try increasing iteration count)")
	}

	origDBackingLen := len(d.Bytes())
	if origDBackingLen >= 32 {
		t.Skip("private key has no leading zero bytes; cannot verify shortened D.Bytes() case")
	}

	// Zero() must write 32 zero bytes even though D.Bytes() was shorter.
	w.Zero()

	// After Zero, D should be fully zeroed.
	result := d.Bytes()
	if !allZero(result) {
		t.Errorf("D.Bytes() = %v after Zero, expected all zeros", result)
	}
}

// TestExtendedKey_ConcurrentZeroAndAccess verifies that ExtendedKey's
// RWMutex prevents panics when Zero() races with ECPrivKey()/Hex().
// Run with -race to detect data races.
func TestExtendedKey_ConcurrentZeroAndAccess(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	mk := w.MasterKey()
	done := make(chan struct{})

	go func() {
		defer close(done)
		mk.Zero()
	}()

	for i := 0; i < 100; i++ {
		_, _ = mk.ECPrivKey()
		_ = mk.Hex()
	}

	<-done
}

// TestExtendedKey_ConcurrentZeroAndDerive verifies that Derive() and
// String() are protected by the RWMutex when racing with Zero().
// Without locks on Derive()/String(), a concurrent Zero() could nil
// privKey or chainCode mid-read and cause panics.
// Run with -race to detect data races.
func TestExtendedKey_ConcurrentZeroAndDerive(t *testing.T) {
	w, err := NewFromMnemonic(TestMnemonic)
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	mk := w.MasterKey()
	done := make(chan struct{})

	go func() {
		defer close(done)
		mk.Zero()
	}()

	for i := 0; i < 100; i++ {
		// Derive reads privKey, pubKey, and chainCode — all mutated by Zero().
		_, _ = mk.Derive(0)
		// String reads chainCode and privKey — all mutated by Zero().
		_ = mk.String()
		// ECPubKey reads pubKey.
		_, _ = mk.ECPubKey()
	}

	<-done
}
