package wallet

import (
	"crypto/sha256"

	"github.com/lbryio/lbcd/btcec"
	"golang.org/x/crypto/ripemd160"
)

// LBRY chain constants. These mirror lbcd/chaincfg.MainNetParams but without
// importing the chaincfg package (which drags in wire + spew).
// Source of truth: lbcd/chaincfg/params.go MainNetParams.

const (
	// P2PKHAddrID is the version byte for LBRY pay-to-pubkey-hash addresses.
	// LBRY mainnet addresses start with 'b'.
	P2PKHAddrID byte = 0x55

	// WIFPrivKeyID is the version byte for wallet import format private keys.
	WIFPrivKeyID byte = 0x1c

	// SLIP44CoinType is the BIP44 coin type registered for LBRY in SLIP-44.
	// Not used for derivation (LBRY uses m/chain/index, not BIP44) but
	// included for reference and HRP-based address schemes.
	SLIP44CoinType uint32 = 293
)

// Address represents a LBRY P2PKH address.
type Address struct {
	hash160 [20]byte
}

// NewAddressPubKeyHash creates an Address from a 20-byte HASH160.
func NewAddressPubKeyHash(hash160 [20]byte) *Address {
	return &Address{hash160: hash160}
}

// EncodeAddress returns the Base58Check-encoded address string.
func (a *Address) EncodeAddress() string {
	data := []byte{P2PKHAddrID}
	data = append(data, a.hash160[:]...)
	return base58CheckEncode(data)
}

// String returns the encoded address (same as EncodeAddress).
func (a *Address) String() string { return a.EncodeAddress() }

// Hash160 returns the 20-byte HASH160 of the address.
func (a *Address) Hash160() [20]byte { return a.hash160 }

// ScriptAddress returns the 20-byte HASH160 (matches lbcutil.Address interface).
func (a *Address) ScriptAddress() []byte { return a.hash160[:] }

// AddressFromPubKey derives a P2PKH address from a public key.
// HASH160 = RIPEMD160(SHA256(compressed_pubkey))
func AddressFromPubKey(pubKey *btcec.PublicKey) *Address {
	compressed := pubKey.SerializeCompressed()
	h160 := hash160(compressed)
	return &Address{hash160: h160}
}

// AddressFromPrivKey derives a P2PKH address from a private key.
func AddressFromPrivKey(privKey *btcec.PrivateKey) *Address {
	return AddressFromPubKey(privKey.PubKey())
}

// PubKeyScript returns the P2PKH scriptPubKey for an address:
// OP_DUP OP_HASH160 <20> OP_EQUALVERIFY OP_CHECKSIG
func (a *Address) PubKeyScript() []byte {
	script := make([]byte, 0, 25)
	script = append(script, 0x76) // OP_DUP
	script = append(script, 0xa9) // OP_HASH160
	script = append(script, 0x14) // push 20 bytes
	script = append(script, a.hash160[:]...)
	script = append(script, 0x88) // OP_EQUALVERIFY
	script = append(script, 0xac) // OP_CHECKSIG
	return script
}

// hash160 computes RIPEMD160(SHA256(data)).
func hash160(data []byte) [20]byte {
	sha := sha256.Sum256(data)
	r := ripemd160.New()
	r.Write(sha[:])
	sum := r.Sum(nil)
	var result [20]byte
	copy(result[:], sum)
	return result
}

// hash256 computes double-SHA256 (SHA256d).
func hash256(data []byte) []byte {
	h := sha256.Sum256(data)
	h2 := sha256.Sum256(h[:])
	return h2[:]
}
