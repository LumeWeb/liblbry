package liblbry

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"fmt"

	liblbcrypto "go.lumeweb.com/liblbry/crypto"
	"go.lumeweb.com/liblbry/errors"
)

const (
)

// Blob represents a data blob with encryption capabilities
type Blob []byte

// NewRawBlob creates a new Blob from data
func NewRawBlob(data []byte) Blob {
	return Blob(data)
}

// Encrypt encrypts the blob data using AES-CBC with PKCS7 padding
func (b Blob) Encrypt(key, iv []byte) ([]byte, error) {
	if len(key) != liblbcrypto.AES128KeySize && len(key) != liblbcrypto.AES192KeySize && len(key) != liblbcrypto.AES256KeySize {
		return nil, fmt.Errorf("key must be %d, %d, or %d bytes for AES-128, AES-192, or AES-256", liblbcrypto.AES128KeySize, liblbcrypto.AES192KeySize, liblbcrypto.AES256KeySize)
	}
	
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("IV must be %d bytes", aes.BlockSize)
	}
	
	if len(b) == 0 {
		return nil, fmt.Errorf("plaintext data must not be empty")
	}
	
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	
	// Apply PKCS7 padding
	paddedData, err := pkcs7Pad([]byte(b), aes.BlockSize)
	if err != nil {
		return nil, fmt.Errorf("failed to pad data: %w", err)
	}
	
	// Encrypt the data
	mode := cipher.NewCBCEncrypter(block, iv)
	encryptedData := make([]byte, len(paddedData))
	mode.CryptBlocks(encryptedData, paddedData)
	
	return encryptedData, nil
}

// Decrypt decrypts the blob data using AES-CBC
func (b Blob) Decrypt(key, iv []byte) ([]byte, error) {
	if len(key) != liblbcrypto.AES128KeySize && len(key) != liblbcrypto.AES192KeySize && len(key) != liblbcrypto.AES256KeySize {
		return nil, fmt.Errorf("key must be %d, %d, or %d bytes for AES-128, AES-192, or AES-256", liblbcrypto.AES128KeySize, liblbcrypto.AES192KeySize, liblbcrypto.AES256KeySize)
	}
	
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("IV must be %d bytes", aes.BlockSize)
	}
	
	if len(b) == 0 {
		return nil, fmt.Errorf("ciphertext data must not be empty")
	}
	
	if len(b) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext data must be at least %d bytes", aes.BlockSize)
	}
	
	if len(b)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext data length must be a multiple of %d bytes", aes.BlockSize)
	}
	
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	
	// Decrypt the data
	mode := cipher.NewCBCDecrypter(block, iv)
	decryptedData := make([]byte, len(b))
	mode.CryptBlocks(decryptedData, b)
	
	// Remove PKCS7 padding
	unpaddedData, err := pkcs7Unpad(decryptedData, aes.BlockSize)
	if err != nil {
		return nil, fmt.Errorf("failed to unpad data: %w", err)
	}
	
	return unpaddedData, nil
}

// pkcs7Pad applies PKCS7 padding to the data
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

// pkcs7Unpad removes PKCS7 padding from the data
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

// BlobStore defines the interface for blob storage operations
type BlobStore interface {
	Has(hash string) (bool, error)
	Get(hash string) ([]byte, error)
	Put(hash string, data []byte) error
	PutSD(hash string, data []byte) error
	Name() string
}

// BlobTransfer defines the interface for blob acquisition/transfer
type BlobTransfer interface {
	Get(hash string) ([]byte, error)
	Name() string
}
