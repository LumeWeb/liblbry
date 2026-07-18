package wallet

import (
	"encoding/hex"
	"testing"
)

// TestBIP32_Vector1 verifies our BIP32 implementation matches lbcutil's
// output for the standard BIP32 test vector 1.
//
// Reference (from lbcutil/hdkeychain):
//   seed: 000102030405060708090a0b0c0d0e0f
//   master xprv: xprv9s21ZrQH143K3QTDL4LXw2F7HEK3wJUD2nW2nRk4stbPy6cq3jPPqjiChkVvvNKmPGJxWUtg6LnF5kejMRNNU3TGtRBeJgk33yuGBxrMPHi
//   m/0' xprv:    xprv9uHRZZhk6KAJC1avXpDAp4MDc3sQKNxDiPvvkX8Br5ngLNv1TxvUxt4cV1rGL5hj6KCesnDYUhd7oWgT11eZG7XnxHrnYeSvkzY7d2bhkJ7
//   m/0'/1 xprv:  xprv9wTYmMFdV23N2TdNG573QoEsfRrWKQgWeibmLntzniatZvR9BmLnvSxqu53Kw1UmYPxLgboyZQaXwTCg8MSY3H2EU4pWcQDnRnrVA1xe8fs
func TestBIP32_Vector1(t *testing.T) {
	seed, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")

	master, err := NewMaster(seed)
	if err != nil {
		t.Fatalf("NewMaster: %v", err)
	}

	wantMaster := "xprv9s21ZrQH143K3QTDL4LXw2F7HEK3wJUD2nW2nRk4stbPy6cq3jPPqjiChkVvvNKmPGJxWUtg6LnF5kejMRNNU3TGtRBeJgk33yuGBxrMPHi"
	if got := master.String(); got != wantMaster {
		t.Errorf("master xprv mismatch:\n  got:  %s\n  want: %s", got, wantMaster)
	}

	// m/0' (hardened)
	child, err := master.Derive(HardenedKeyStart + 0)
	if err != nil {
		t.Fatalf("Derive m/0': %v", err)
	}
	wantChild := "xprv9uHRZZhk6KAJC1avXpDAp4MDc3sQKNxDiPvvkX8Br5ngLNv1TxvUxt4cV1rGL5hj6KCesnDYUhd7oWgT11eZG7XnxHrnYeSvkzY7d2bhkJ7"
	if got := child.String(); got != wantChild {
		t.Errorf("m/0' xprv mismatch:\n  got:  %s\n  want: %s", got, wantChild)
	}

	// m/0'/1
	child2, err := child.Derive(1)
	if err != nil {
		t.Fatalf("Derive m/0'/1: %v", err)
	}
	wantChild2 := "xprv9wTYmMFdV23N2TdNG573QoEsfRrWKQgWeibmLntzniatZvR9BmLnvSxqu53Kw1UmYPxLgboyZQaXwTCg8MSY3H2EU4pWcQDnRnrVA1xe8fs"
	if got := child2.String(); got != wantChild2 {
		t.Errorf("m/0'/1 xprv mismatch:\n  got:  %s\n  want: %s", got, wantChild2)
	}

	// Verify the private key at m/0'/1
	priv, err := child2.ECPrivKey()
	if err != nil {
		t.Fatalf("ECPrivKey: %v", err)
	}
	wantPriv := "3c6cb8d0f6a264c91ea8b5030fadaa8e538b020f0a387421a12de9319dc93368"
	if got := hex.EncodeToString(priv.Serialize()); got != wantPriv {
		t.Errorf("m/0'/1 priv mismatch:\n  got:  %s\n  want: %s", got, wantPriv)
	}
}
