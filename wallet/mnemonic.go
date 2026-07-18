// Package wallet implements LBRY HD wallet creation and key management.
//
// LBRY uses Electrum's mnemonic format (not standard BIP39) with simple
// BIP32 derivation: m/chain/index (non-hardened), where chain 0=receiving,
// 1=change, 2=channel. The passphrase for seed derivation is "lbryum".
//
// All cryptographic operations use lbcd's btcec (secp256k1). BIP32 key
// derivation is implemented locally to avoid pulling in lbcutil/hdkeychain
// (which transitively depends on PebbleDB and other heavy storage deps).

package wallet

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/unicode/norm"
)

// SeedPrefix is the standard Electrum/LBRY wallet seed prefix.
var SeedPrefix = []byte("01")

// Passphrase is the standard passphrase used by the LBRY ecosystem for
// seed derivation.
const Passphrase = "lbryum"

// wordIndexMap maps word to index for O(1) lookup.
var wordIndexMap = buildWordIndexMap()

func buildWordIndexMap() map[string]int {
	m := make(map[string]int, len(wordlist))
	for i, w := range wordlist {
		m[w] = i
	}
	return m
}

// NormalizeText normalizes a mnemonic seed string.
// Port of lbry-sdk's normalize_text():
//   - NFKD normalization
//   - lowercase
//   - remove combining marks (accents)
//   - normalize whitespace (collapse to single spaces)
//   - remove whitespace between CJK characters
func NormalizeText(seed string) string {
	// NFKD + lowercase + remove combining marks
	var b strings.Builder
	for _, r := range norm.NFKD.String(seed) {
		if unicode.Is(unicode.Mn, r) {
			continue // skip combining marks
		}
		b.WriteRune(r)
	}
	seed = strings.ToLower(b.String())

	// Normalize whitespace
	seed = strings.Join(strings.Fields(seed), " ")

	// Remove whitespace between CJK characters
	runes := []rune(seed)
	var out []rune
	for i, r := range runes {
		if isWhitespace(r) && i > 0 && i < len(runes)-1 {
			if isCJK(runes[i-1]) && isCJK(runes[i+1]) {
				continue
			}
		}
		out = append(out, r)
	}
	return string(out)
}

func isWhitespace(r rune) bool {
	return unicode.IsSpace(r)
}

// isCJK checks if a rune is in a CJK unicode interval.
func isCJK(r rune) bool {
	intervals := [][2]rune{
		{0x4E00, 0x9FFF},
		{0x3400, 0x4DBF},
		{0x20000, 0x2A6DF},
		{0x2A700, 0x2B73F},
		{0x2B740, 0x2B81F},
		{0xF900, 0xFAFF},
		{0x2F800, 0x2FA1D},
		{0x3190, 0x319F},
		{0x2E80, 0x2EFF},
		{0x2F00, 0x2FDF},
		{0x31C0, 0x31EF},
		{0x2FF0, 0x2FFF},
		{0xE0100, 0xE01EF},
		{0x3100, 0x312F},
		{0x31A0, 0x31BF},
		{0xFF00, 0xFFEF},
		{0x3040, 0x309F},
		{0x30A0, 0x30FF},
		{0x31F0, 0x31FF},
		{0x1B000, 0x1B0FF},
		{0xAC00, 0xD7AF},
		{0x1100, 0x11FF},
		{0xA960, 0xA97F},
		{0xD7B0, 0xD7FF},
		{0x3130, 0x318F},
		{0xA4D0, 0xA4FF},
		{0x16F00, 0x16F9F},
		{0xA000, 0xA48F},
		{0xA490, 0xA4CF},
	}
	for _, iv := range intervals {
		if r >= iv[0] && r <= iv[1] {
			return true
		}
	}
	return false
}

// MnemonicToSeed derives a 64-byte seed from a mnemonic phrase and passphrase.
// Uses PBKDF2-HMAC-SHA512 with 2048 iterations, same as BIP39.
// Port of lbry-sdk's Mnemonic.mnemonic_to_seed().
func MnemonicToSeed(mnemonic, passphrase string) []byte {
	mnemonic = NormalizeText(mnemonic)
	passphrase = NormalizeText(passphrase)
	return pbkdf2.Key([]byte(mnemonic), []byte(passphrase), 2048, 64, sha512.New)
}

// IsNewSeed checks if a mnemonic's HMAC-SHA512("Seed version", seed) hash starts
// with the given prefix. Port of lbry-sdk's is_new_seed().
func IsNewSeed(seed string, prefix []byte) bool {
	seed = NormalizeText(seed)
	mac := hmac.New(sha512.New, []byte("Seed version"))
	mac.Write([]byte(seed))
	hash := hex.EncodeToString(mac.Sum(nil))
	return strings.HasPrefix(hash, string(prefix))
}

// MnemonicEncode converts an integer to a mnemonic phrase.
// Port of lbry-sdk's Mnemonic.mnemonic_encode().
func MnemonicEncode(i *big.Int) string {
	n := big.NewInt(int64(len(wordlist)))
	var words []string
	zero := big.NewInt(0)
	mod := new(big.Int)
	temp := new(big.Int).Set(i)

	for temp.Cmp(zero) > 0 {
		temp.DivMod(temp, n, mod)
		words = append(words, wordlist[mod.Int64()])
	}
	return strings.Join(words, " ")
}

// MnemonicDecode converts a mnemonic phrase back to an integer.
// Port of lbry-sdk's Mnemonic.mnemonic_decode().
func MnemonicDecode(seed string) (*big.Int, error) {
	seed = NormalizeText(seed)
	words := strings.Fields(seed)
	n := big.NewInt(int64(len(wordlist)))
	i := big.NewInt(0)

	// Process words in reverse (pop from end)
	for j := len(words) - 1; j >= 0; j-- {
		idx, ok := wordIndexMap[words[j]]
		if !ok {
			return nil, fmt.Errorf("word %q not in wordlist", words[j])
		}
		i.Mul(i, n)
		i.Add(i, big.NewInt(int64(idx)))
	}
	return i, nil
}

// MakeSeed generates a new mnemonic seed phrase that passes IsNewSeed.
// Port of lbry-sdk's Mnemonic.make_seed().
func MakeSeed() (string, error) {
	return MakeSeedWithPrefix(SeedPrefix)
}

// MakeSeedWithPrefix generates a new mnemonic with a custom prefix.
func MakeSeedWithPrefix(prefix []byte) (string, error) {
	const numBits = 132
	bpw := math.Log2(float64(len(wordlist)))

	// Round up to nearest multiple of bpw
	n := int(math.Ceil(float64(numBits)/bpw) * bpw)

	maxEntropy := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(n)), nil)
	minEntropy := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(n-int(bpw))), nil)

	var entropy *big.Int
	for {
		buf := make([]byte, (n+7)/8)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate entropy: %w", err)
		}
		entropy = new(big.Int).SetBytes(buf)
		entropy.And(entropy, new(big.Int).Sub(maxEntropy, big.NewInt(1)))

		if entropy.Cmp(big.NewInt(0)) > 0 && entropy.Cmp(minEntropy) < 0 {
			continue
		}
		if entropy.Cmp(big.NewInt(0)) > 0 && entropy.Cmp(maxEntropy) < 0 {
			break
		}
	}

	nonce := 0
	for {
		nonce++
		i := new(big.Int).Add(entropy, big.NewInt(int64(nonce)))
		seed := MnemonicEncode(i)

		// Verify round-trip
		decoded, err := MnemonicDecode(seed)
		if err != nil {
			return "", err
		}
		if decoded.Cmp(i) != 0 {
			return "", fmt.Errorf("cannot extract same entropy from mnemonic")
		}

		if IsNewSeed(seed, prefix) {
			return seed, nil
		}
	}
}

// IsValid checks if a mnemonic is valid by verifying all words exist in the
// wordlist and the Electrum seed checksum passes.
func IsValid(seed string) bool {
	seed = NormalizeText(seed)
	words := strings.Fields(seed)
	if len(words) == 0 {
		return false
	}
	for _, w := range words {
		if _, ok := wordIndexMap[w]; !ok {
			return false
		}
	}
	return IsNewSeed(seed, SeedPrefix)
}
