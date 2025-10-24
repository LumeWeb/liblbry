// Code copied from github.com/lbryio/lbry.go/v2 - MIT License (c) 2016-2020 LBRY Inc.
//
// Copied for liblbry integration without functional changes:
//   - Preserved exact PKCS7 padding implementation for compatibility
//   - Maintained identical blob encryption/decryption behavior
//   - Kept core LBRY blob handling and validation logic

package stream

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/hex"

	"go.lumeweb.com/liblbry/errors"
	liblbry "go.lumeweb.com/liblbry"
)

const (
	MaxBlobSize       = 2097152 // 2mb, or 2 * 2^20
	BlobHashSize      = sha512.Size384
	BlobHashHexLength = BlobHashSize * 2 // in hex, each byte is 2 chars

	// AES key lengths in bytes
	AES128KeySize = 16 // AES-128
	AES192KeySize = 24 // AES-192
	AES256KeySize = 32 // AES-256
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
		return liblbry.ErrBlobTooBig
	}
	if b.Size() == 0 {
		return liblbry.ErrBlobEmpty
	}
	return nil
}

func NewBlob(data, key, iv []byte) (Blob, error) {
	if len(data) == 0 {
		// this is here to match python behavior. in theory we could encrypt an empty blob
		return nil, errors.Err("cannot encrypt empty slice")
	}
	
	// Validate key length - AES supports 16, 24, or 32 bytes
	if len(key) != AES128KeySize && len(key) != AES192KeySize && len(key) != AES256KeySize {
		return nil, errors.Err("invalid key length %d, must be %d, %d, or %d bytes", len(key), AES128KeySize, AES192KeySize, AES256KeySize)
	}
	
	blockCipher, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Err(err)
	}
	
	// Validate IV length
	if len(iv) != aes.BlockSize {
		return nil, errors.Err("IV length must equal %d bytes, got %d", aes.BlockSize, len(iv))
	}

	cbc := cipher.NewCBCEncrypter(blockCipher, iv)
	plaintext, err := pkcs7Pad(data, blockCipher.BlockSize())
	if err != nil {
		return nil, errors.Err(err)
	}

	// Validate plaintext length before encryption
	if len(plaintext) == 0 || len(plaintext)%aes.BlockSize != 0 {
		return nil, errors.Err("invalid plaintext length %d, must be non-zero multiple of %d", len(plaintext), aes.BlockSize)
	}

	ciphertext := make([]byte, len(plaintext))
	cbc.CryptBlocks(ciphertext, plaintext)
	return ciphertext, nil
}

// DecryptBlob decrypts a blob using the provided key and IV.
// It validates the key length (16, 24, or 32 bytes), IV length (must equal aes.BlockSize),
// and ciphertext length before decryption. All validation errors are returned, no panics.
func DecryptBlob(b Blob, key, iv []byte) ([]byte, error) {
	return b.Plaintext(key, iv)
}

func (b Blob) Plaintext(key, iv []byte) ([]byte, error) {
	// Validate key length - AES supports 16, 24, or 32 bytes
	if len(key) != AES128KeySize && len(key) != AES192KeySize && len(key) != AES256KeySize {
		return nil, errors.Err("invalid key length %d, must be %d, %d, or %d bytes", len(key), AES128KeySize, AES192KeySize, AES256KeySize)
	}
	
	blockCipher, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Err(err)
	}
	
	// Validate IV length
	if len(iv) != aes.BlockSize {
		return nil, errors.Err("IV length must equal %d bytes, got %d", aes.BlockSize, len(iv))
	}
	
	// Validate ciphertext length before decryption
	if len(b) <= 0 {
		return nil, errors.Err("ciphertext length must be greater than 0, got %d", len(b))
	}
	if len(b)%blockCipher.BlockSize() != 0 {
		return nil, errors.Err("ciphertext length %d is not a multiple of block size %d", len(b), blockCipher.BlockSize())
	}

	cbc := cipher.NewCBCDecrypter(blockCipher, iv)
	plaintext := make([]byte, len(b))
	cbc.CryptBlocks(plaintext, b)

	plaintext, err = pkcs7Unpad(plaintext, blockCipher.BlockSize())
	if err != nil {
		return nil, errors.Err(err)
	}

	return plaintext, nil
}

// https://github.com/fullsailor/pkcs7/blob/master/pkcs7.go#L468
func pkcs7Pad(data []byte, blockLen int) ([]byte, error) {
	if blockLen < 1 {
		return nil, errors.Err("invalid block length %d", blockLen)
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
		return nil, errors.Err("invalid block length %d", blockLen)
	}
	if len(data)%blockLen != 0 || len(data) == 0 {
		return nil, errors.Err("invalid data length %d", len(data))
	}

	// the last byte is the length of padding
	padLen := int(data[len(data)-1])

	// Validate padLen to prevent out-of-range slicing
	if padLen < 1 {
		return nil, errors.Err("invalid padding length %d, must be at least 1", padLen)
	}
	if padLen > blockLen {
		return nil, errors.Err("invalid padding length %d, must be <= block size %d", padLen, blockLen)
	}
	if padLen > len(data) {
		return nil, errors.Err("invalid padding length %d, must be <= data length %d", padLen, len(data))
	}

	// check padding integrity, all bytes should be the same
	pad := data[len(data)-padLen:]
	for _, padbyte := range pad {
		if padbyte != byte(padLen) {
			return nil, errors.Err("invalid padding")
		}
	}

	return data[:len(data)-padLen], nil
}
