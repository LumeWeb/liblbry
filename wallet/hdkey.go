package wallet

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync"

	"github.com/lbryio/lbcd/btcec"
	base58 "github.com/mr-tron/base58"
)

// HardenedKeyStart is the index where hardened child keys begin (BIP32).
const HardenedKeyStart uint32 = 0x80000000

// masterKey is the HMAC key used in BIP32 master key derivation.
var masterKey = []byte("Bitcoin seed")

// ExtendedKey implements BIP32 hierarchical deterministic key derivation
// using only lbcd/btcec. This replaces lbcutil/hdkeychain to avoid the
// transitive dependency on PebbleDB and other heavy storage backends.
//
// LBRY uses only non-hardened derivation (m/chain/index), but hardened
// derivation is supported for completeness.
type ExtendedKey struct {
	mu        sync.RWMutex
	privKey   *btcec.PrivateKey
	pubKey    *btcec.PublicKey
	chainCode []byte
	parentFP  [4]byte
	depth     uint8
	childNum  uint32
	isPrivate bool
}

// withRLock runs fn under the extended key's read lock.
func (k *ExtendedKey) withRLock(fn func() error) error {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return fn()
}

// withLock runs fn under the extended key's write lock.
func (k *ExtendedKey) withLock(fn func()) {
	k.mu.Lock()
	defer k.mu.Unlock()
	fn()
}

// fingerprint returns the first 4 bytes of RIPEMD160(SHA256(pubkey)),
// matching BIP32 parent fingerprint semantics.
func fingerprint(pubKey *btcec.PublicKey) [4]byte {
	h := hash160(pubKey.SerializeCompressed())
	var fp [4]byte
	copy(fp[:], h[:4])
	return fp
}

// NewMaster creates a new BIP32 master extended key from a seed.
// The seed should be 32-64 bytes (typically 64 from PBKDF2).
func NewMaster(seed []byte) (*ExtendedKey, error) {
	if len(seed) < 16 || len(seed) > 64 {
		return nil, fmt.Errorf("invalid seed length: %d (must be 16-64)", len(seed))
	}

	// I = HMAC-SHA512(Key = "Bitcoin seed", Data = seed)
	mac := hmac.New(sha512.New, masterKey)
	mac.Write(seed)
	lr := mac.Sum(nil)

	secretKey := lr[:32]
	chainCode := lr[32:]

	// Ensure the key is usable (0 < key < N)
	keyNum := new(big.Int).SetBytes(secretKey)
	if keyNum.Sign() == 0 || keyNum.Cmp(btcec.S256().N) >= 0 {
		return nil, fmt.Errorf("unusable seed: derived key is out of range")
	}

	priv, _ := btcec.PrivKeyFromBytes(btcec.S256(), secretKey)

	return &ExtendedKey{
		privKey:   priv,
		pubKey:    priv.PubKey(),
		chainCode: chainCode,
		depth:     0,
		childNum:  0,
		isPrivate: true,
	}, nil
}

// Derive derives a child key at the given index.
// For hardened derivation, use index + HardenedKeyStart.
func (k *ExtendedKey) Derive(i uint32) (child *ExtendedKey, err error) {
	k.withRLock(func() error {
		child, err = k.deriveLocked(i)
		return err
	})
	return
}

// deriveLocked is the lock-free inner implementation of Derive.
// Caller must hold k.mu.RLock() (or k.mu.Lock()).
func (k *ExtendedKey) deriveLocked(i uint32) (*ExtendedKey, error) {
	if k.depth == 255 {
		return nil, fmt.Errorf("derivation depth exceeds maximum (255)")
	}

	isHardened := i >= HardenedKeyStart

	// Cannot derive hardened children from a public key
	if !k.isPrivate && isHardened {
		return nil, fmt.Errorf("cannot derive hardened child from public key")
	}

	// Check for zeroed key before accessing fields.
	if isHardened && k.privKey == nil {
		return nil, fmt.Errorf("extended key has been zeroed")
	}
	if k.pubKey == nil {
		return nil, fmt.Errorf("extended key has been zeroed")
	}
	if k.chainCode == nil {
		return nil, fmt.Errorf("extended key has been zeroed")
	}

	// Build the data for HMAC-SHA512
	var data []byte
	if isHardened {
		// 0x00 || ser256(parentPrivKey) || ser32(i)
		data = make([]byte, 37)
		copy(data[1:], k.privKey.Serialize())
	} else {
		// serP(parentPubKey) || ser32(i)
		data = make([]byte, 37)
		copy(data, k.pubKey.SerializeCompressed())
	}
	binary.BigEndian.PutUint32(data[33:], i)

	// I = HMAC-SHA512(Key = chainCode, Data = data)
	mac := hmac.New(sha512.New, k.chainCode)
	mac.Write(data)
	ilr := mac.Sum(nil)

	il := ilr[:32]
	childChainCode := ilr[32:]

	// Ensure Il is a valid private key (0 < Il < N)
	ilNum := new(big.Int).SetBytes(il)
	if ilNum.Sign() == 0 || ilNum.Cmp(btcec.S256().N) >= 0 {
		return nil, fmt.Errorf("invalid child at index %d (extremely rare, try next)", i)
	}

	if k.isPrivate {
		if k.privKey == nil {
			return nil, fmt.Errorf("extended key has been zeroed")
		}
		// childKey = (parse256(Il) + parentKey) mod N
		keyNum := new(big.Int).SetBytes(k.privKey.Serialize())
		ilNum.Add(ilNum, keyNum)
		ilNum.Mod(ilNum, btcec.S256().N)

		childBytes := ilNum.Bytes()
		// Pad to 32 bytes
		padded := make([]byte, 32)
		copy(padded[32-len(childBytes):], childBytes)

		priv, _ := btcec.PrivKeyFromBytes(btcec.S256(), padded)
		return &ExtendedKey{
			privKey:   priv,
			pubKey:    priv.PubKey(),
			chainCode: childChainCode,
			parentFP:  fingerprint(k.pubKey),
			depth:     k.depth + 1,
			childNum:  i,
			isPrivate: true,
		}, nil
	}

	// Public child derivation: childKey = point(parse256(Il)) + parentPubKey
	ilx, ily := btcec.S256().ScalarBaseMult(il)
	if ilx.Sign() == 0 || ily.Sign() == 0 {
		return nil, fmt.Errorf("invalid child at index %d", i)
	}

	childX, childY := btcec.S256().Add(
		k.pubKey.ToECDSA().X, k.pubKey.ToECDSA().Y,
		ilx, ily,
	)

	childPub := &btcec.PublicKey{
		Curve: btcec.S256(),
		X:     childX,
		Y:     childY,
	}

	return &ExtendedKey{
		privKey:   nil,
		pubKey:    childPub,
		chainCode: childChainCode,
		parentFP:  fingerprint(k.pubKey),
		depth:     k.depth + 1,
		childNum:  i,
		isPrivate: false,
	}, nil
}

// ECPrivKey returns the private key.
func (k *ExtendedKey) ECPrivKey() (pk *btcec.PrivateKey, err error) {
	k.withRLock(func() error {
		if !k.isPrivate {
			return fmt.Errorf("extended key is public-only")
		}
		if k.privKey == nil {
			return fmt.Errorf("extended key has been zeroed")
		}
		pk = k.privKey
		return nil
	})
	return
}

// ECPubKey returns the public key.
func (k *ExtendedKey) ECPubKey() (pk *btcec.PublicKey, err error) {
	k.withRLock(func() error { pk = k.pubKey; return nil })
	return
}

// IsPrivate returns whether this extended key has a private key.
func (k *ExtendedKey) IsPrivate() (b bool) {
	k.withRLock(func() error { b = k.isPrivate; return nil })
	return
}

// Depth returns the depth in the HD tree (0 = master).
func (k *ExtendedKey) Depth() (d uint8) {
	k.withRLock(func() error { d = k.depth; return nil })
	return
}

// ChildNum returns the child number at this depth.
func (k *ExtendedKey) ChildNum() (n uint32) {
	k.withRLock(func() error { n = k.childNum; return nil })
	return
}

// String returns the Base58Check-encoded extended key (xprv/xpub format).
// Returns "" if the key has been zeroed (chain code or private key is nil).
func (k *ExtendedKey) String() (s string) {
	k.withRLock(func() error {
		var buf bytes.Buffer

		// Version (4 bytes)
		if k.isPrivate {
			buf.Write([]byte{0x04, 0x88, 0xad, 0xe4}) // xprv
		} else {
			buf.Write([]byte{0x04, 0x88, 0xb2, 0x1e}) // xpub
		}

		// Depth (1 byte)
		buf.WriteByte(k.depth)

		// Parent fingerprint (4 bytes)
		buf.Write(k.parentFP[:])

		// Child number (4 bytes)
		var childNumBytes [4]byte
		binary.BigEndian.PutUint32(childNumBytes[:], k.childNum)
		buf.Write(childNumBytes[:])

		// Chain code (32 bytes)
		if k.chainCode == nil {
			return nil // return ""
		}
		buf.Write(k.chainCode)

		// Key data (33 bytes)
		if k.isPrivate {
			buf.WriteByte(0x00)
			if k.privKey == nil {
				return nil // return ""
			}
			buf.Write(k.privKey.Serialize())
		} else {
			buf.Write(k.pubKey.SerializeCompressed())
		}

		s = base58CheckEncode(buf.Bytes())
		return nil
	})
	return
}

// Hex returns the private key as a hex string.
func (k *ExtendedKey) Hex() (s string) {
	k.withRLock(func() error {
		if !k.isPrivate || k.privKey == nil {
			return nil // return ""
		}
		s = hex.EncodeToString(k.privKey.Serialize())
		return nil
	})
	return
}

// Zero securely wipes the private key scalar and chain code from memory.
// After calling Zero, the extended key is unusable for private operations.
func (k *ExtendedKey) Zero() {
	k.withLock(func() {
		if k.privKey != nil {
			wipeBigIntD(k.privKey)
			k.privKey = nil
		}
		if k.chainCode != nil {
			for i := range k.chainCode {
				k.chainCode[i] = 0
			}
			k.chainCode = nil
		}
	})
}

// base58CheckEncode appends a 4-byte double-SHA256 checksum and Base58-encodes.
func base58CheckEncode(data []byte) string {
	h := sha256.Sum256(data)
	h2 := sha256.Sum256(h[:])
	checksum := h2[:4]
	return base58.Encode(append(data, checksum...))
}
