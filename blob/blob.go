// Code copied from github.com/lbryio/lbry.go/v2 - MIT License (c) 2016-2020 LBRY Inc.
//
// Copied for liblbry integration without functional changes:
//   - Preserved exact PKCS7 padding implementation for compatibility
//   - Maintained identical blob encryption/decryption behavior
//   - Kept core LBRY blob handling and validation logic

package blob

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/hex"

	liblbcrypto "go.lumeweb.com/liblbry/crypto"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
)

const (
	MaxBlobSize       = 2097152 // 2mb, or 2 * 2^20
	BlobHashSize      = sha512.Size384
	BlobHashHexLength = BlobHashSize * 2 // in hex, each byte is 2 chars
)

// Blob represents a data blob with encryption capabilities
type Blob []byte

func (b Blob) Size() int {
	return len(b)
}

// Hash returns a hash of the blob data
func (b Blob) Hash() []byte {
	if b.Size() == 0 {
		return nil
	}
	hashBytes := sha512.Sum384(b)
	return hashBytes[:]
}

// HashHex returns the blob hash as a hex string
func (b Blob) HashHex() string {
	return hex.EncodeToString(b.Hash())
}

// ValidForSend returns true if the blob size is within the limits
func (b Blob) ValidForSend() error {
	if b.Size() > MaxBlobSize {
		return liblbryerrors.ErrBlobTooBig
	}
	if b.Size() == 0 {
		return liblbryerrors.ErrBlobEmpty
	}
	return nil
}

func NewBlob(data, key, iv []byte) (Blob, error) {
	if len(data) == 0 {
		// this is here to match python behavior. in theory we could encrypt an empty blob
		return nil, liblbryerrors.Err("cannot encrypt empty slice")
	}

	ciphertext, err := encryptData(data, key, iv)
	if err != nil {
		return nil, err
	}
	return ciphertext, nil
}

// encryptData handles the core encryption logic
func encryptData(data, key, iv []byte) ([]byte, error) {
	// Validate key length - AES supports 16, 24, or 32 bytes
	if len(key) != liblbcrypto.AES128KeySize && len(key) != liblbcrypto.AES192KeySize && len(key) != liblbcrypto.AES256KeySize {
		return nil, liblbryerrors.Err("invalid key length %d, must be %d, %d, or %d bytes", len(key), liblbcrypto.AES128KeySize, liblbcrypto.AES192KeySize, liblbcrypto.AES256KeySize)
	}

	blockCipher, err := aes.NewCipher(key)
	if err != nil {
		return nil, liblbryerrors.Err(err)
	}

	blockSize := blockCipher.BlockSize()

	// Validate IV length
	if len(iv) != blockSize {
		return nil, liblbryerrors.Err("IV length must equal %d bytes, got %d", blockSize, len(iv))
	}

	cbc := cipher.NewCBCEncrypter(blockCipher, iv)
	plaintext, err := pkcs7Pad(data, blockSize)
	if err != nil {
		return nil, liblbryerrors.Err(err)
	}

	// Validate plaintext length before encryption
	if len(plaintext) == 0 || len(plaintext)%blockSize != 0 {
		return nil, liblbryerrors.Err("invalid plaintext length %d, must be non-zero multiple of %d", len(plaintext), blockSize)
	}

	ciphertext := make([]byte, len(plaintext))
	cbc.CryptBlocks(ciphertext, plaintext)
	return ciphertext, nil
}

// DecryptBlob decrypts a blob using the provided key and IV.
// It validates the key length (16, 24, or 32 bytes), IV length (must equal block size),
// and ciphertext length before decryption. All validation errors are returned, no panics.
func DecryptBlob(b Blob, key, iv []byte) ([]byte, error) {
	return b.Plaintext(key, iv)
}

func (b Blob) Plaintext(key, iv []byte) ([]byte, error) {
	return decryptData([]byte(b), key, iv)
}

// decryptData handles the core decryption logic
func decryptData(data, key, iv []byte) ([]byte, error) {
	// Validate key length - AES supports 16, 24, or 32 bytes
	if len(key) != liblbcrypto.AES128KeySize && len(key) != liblbcrypto.AES192KeySize && len(key) != liblbcrypto.AES256KeySize {
		return nil, liblbryerrors.Err("invalid key length %d, must be %d, %d, or %d bytes", len(key), liblbcrypto.AES128KeySize, liblbcrypto.AES192KeySize, liblbcrypto.AES256KeySize)
	}

	blockCipher, err := aes.NewCipher(key)
	if err != nil {
		return nil, liblbryerrors.Err(err)
	}

	blockSize := blockCipher.BlockSize()

	// Validate IV length
	if len(iv) != blockSize {
		return nil, liblbryerrors.Err("IV length must equal %d bytes, got %d", blockSize, len(iv))
	}

	// Validate ciphertext length before decryption
	if len(data) <= 0 || len(data)%blockSize != 0 {
		return nil, liblbryerrors.Err("ciphertext length %d is not a valid multiple of block size %d", len(data), blockSize)
	}

	cbc := cipher.NewCBCDecrypter(blockCipher, iv)
	plaintext := make([]byte, len(data))
	cbc.CryptBlocks(plaintext, data)

	plaintext, err = pkcs7Unpad(plaintext, blockSize)
	if err != nil {
		return nil, liblbryerrors.Err(err)
	}

	return plaintext, nil
}

// Encrypt encrypts the blob data using AES-CBC with PKCS7 padding
func (b Blob) Encrypt(key, iv []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, liblbryerrors.Err("plaintext data must not be empty")
	}

	encryptedData, err := encryptData([]byte(b), key, iv)
	if err != nil {
		return nil, err
	}

	return encryptedData, nil
}

// Decrypt decrypts the blob data using AES-CBC
func (b Blob) Decrypt(key, iv []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, liblbryerrors.Err("ciphertext data must not be empty")
	}

	decryptedData, err := decryptData([]byte(b), key, iv)
	if err != nil {
		return nil, err
	}

	return decryptedData, nil
}

// https://github.com/fullsailor/pkcs7/blob/master/pkcs7.go#L468
func pkcs7Pad(data []byte, blockLen int) ([]byte, error) {
	if blockLen < 1 {
		return nil, liblbryerrors.Err("invalid block length %d", blockLen)
	}
	padLen := blockLen - (len(data) % blockLen)
	if padLen == 0 {
		padLen = blockLen
	}
	padded := make([]byte, len(data)+padLen)
	copy(padded, data)
	copy(padded[len(padded)-padLen:], bytes.Repeat([]byte{byte(padLen)}, padLen))
	return padded, nil
}

func pkcs7Unpad(data []byte, blockLen int) ([]byte, error) {
	if blockLen < 1 {
		return nil, liblbryerrors.Err("invalid block length %d", blockLen)
	}
	if len(data)%blockLen != 0 || len(data) == 0 {
		return nil, liblbryerrors.Err("invalid data length %d", len(data))
	}

	// the last byte is the length of padding
	padLen := int(data[len(data)-1])

	// Validate padLen to prevent out-of-range slicing
	if padLen < 1 {
		return nil, liblbryerrors.Err("invalid padding length %d, must be at least 1", padLen)
	}
	if padLen > blockLen {
		return nil, liblbryerrors.Err("invalid padding length %d, must be <= block size %d", padLen, blockLen)
	}
	if padLen > len(data) {
		return nil, liblbryerrors.Err("invalid padding length %d, must be <= data length %d", padLen, len(data))
	}

	// check padding integrity, all bytes should be the same
	pad := data[len(data)-padLen:]
	for _, padbyte := range pad {
		if padbyte != byte(padLen) {
			return nil, liblbryerrors.Err("invalid padding")
		}
	}

	return data[:len(data)-padLen], nil
}
