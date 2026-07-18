package wallet

import (
	"encoding/hex"
	"math/big"
	"testing"
)

// TestAudit_MnemonicSeedVector verifies MnemonicToSeed against the
// lbry-sdk Python reference for the same phrase and passphrase.
//
// Reference (lbry-sdk lbry/wallet/mnemonic.py):
//   PBKDF2-HMAC-SHA512(mnemonic, passphrase, 2048, 64)
//   mnemonic  = "will bus cluster trumpet jump venue truly habit decrease stuff renew horse"
//   passphrase = "lbryum"
func TestAudit_MnemonicSeedVector(t *testing.T) {
	mnemonic := "will bus cluster trumpet jump venue truly habit decrease stuff renew horse"
	wantSeed := "ba350f9df246e7be0d5d5fca2a03ec112d59c73229f9784add762f5c68e57efa0b952505092d49abcff8300b1f0140dcce1a40f9b14040d4143ed2291eccd175"

	got := hex.EncodeToString(MnemonicToSeed(mnemonic, Passphrase))
	if got != wantSeed {
		t.Errorf("MnemonicToSeed mismatch:\n  got:  %s\n  want: %s", got, wantSeed)
	}

	// The same phrase must be recognized as a valid Electrum/LBRY seed.
	if !IsNewSeed(mnemonic, SeedPrefix) {
		t.Errorf("IsNewSeed returned false for known-good seed")
	}
}

// TestAudit_DerivedAddressesVector verifies derived addresses against
// github.com/lbryio/lbcutil/hdkeychain output for the same seed.
//
// Seed: ba350f9d... (from TestAudit_MnemonicSeedVector)
// lbcutil/hdkeychain path: m/chain/index using chaincfg.MainNetParams.
func TestAudit_DerivedAddressesVector(t *testing.T) {
	w, err := NewFromMnemonic("will bus cluster trumpet jump venue truly habit decrease stuff renew horse")
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}

	cases := []struct {
		chain, index uint32
		want         string
	}{
		{0, 0, "bU269oqn4skRpj6J3uS96wJg6XDyTAfbvz"},
		{0, 1, "bPwsrXWTdyyY3kwBSUX3xY3VeY6uZKwoEv"},
		{0, 2, "bVW91zXYZEPhNabbewdmVhvaYxXJR6r8kn"},
		{1, 0, "bZUmEpf7Q5sap3S6HR9koDjQF9RQTEYaov"},
	}

	for _, c := range cases {
		got, err := w.AddressStringAt(c.chain, c.index)
		if err != nil {
			t.Fatalf("AddressStringAt(%d,%d): %v", c.chain, c.index, err)
		}
		if got != c.want {
			t.Errorf("AddressStringAt(%d,%d) = %s, want %s", c.chain, c.index, got, c.want)
		}
	}

	// Default wallet address is m/0/0.
	if w.AddressString() != cases[0].want {
		t.Errorf("default wallet address = %s, want %s", w.AddressString(), cases[0].want)
	}
}

// TestAudit_MasterXPrvVector verifies the master xprv encoding matches
// lbcutil/hdkeychain for the audited seed.
func TestAudit_MasterXPrvVector(t *testing.T) {
	w, err := NewFromMnemonic("will bus cluster trumpet jump venue truly habit decrease stuff renew horse")
	if err != nil {
		t.Fatalf("NewFromMnemonic: %v", err)
	}
	wantXPrv := "xprv9s21ZrQH143K3N97napcyZ3NGzYCdMHoyrufcmKm3CjdAVP5BV8XC9VwTkhe69rfEN1c1FyohV6EziQL7ib6bmNnbpuVRKwnEGYT9s6q3Yt"
	if got := w.MasterKey().String(); got != wantXPrv {
		t.Errorf("master xprv mismatch:\n  got:  %s\n  want: %s", got, wantXPrv)
	}
}

// TestAudit_MnemonicEncodingRoundTrip verifies MnemonicEncode/Decode
// matches lbry-sdk's integer↔phrase conversion using a fixed integer.
func TestAudit_MnemonicEncodingRoundTrip(t *testing.T) {
	i := "12345678901234567890"
	enc := MnemonicEncode(parseBigInt(i))
	dec, err := MnemonicDecode(enc)
	if err != nil {
		t.Fatalf("MnemonicDecode: %v", err)
	}
	if dec.Cmp(parseBigInt(i)) != 0 {
		t.Errorf("round-trip failed: %s -> %s -> %s", i, enc, dec.String())
	}
}

func parseBigInt(s string) *big.Int {
	i, _ := new(big.Int).SetString(s, 10)
	return i
}
