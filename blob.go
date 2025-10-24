package liblbry

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

const (
	// AES key lengths in bytes
	AES128KeySize = 16 // AES-128
	AES192KeySize = 24 // AES-192
	AES256KeySize = 32 // AES-256
)

// Blob represents a data blob with encryption capabilities
type Blob []byte

// NewBlob creates a new Blob from data
func NewBlob(data []byte) Blob {
	return Blob(data)
}

// Encrypt encrypts the blob data using AES-CBC with PKCS7 padding
func (b Blob) Encrypt(key, iv []byte) ([]byte, error) {
	if len(key) != AES128KeySize && len(key) != AES192KeySize && len(key) != AES256KeySize {
		return nil, fmt.Errorf("key must be %d, %d, or %d bytes for AES-128, AES-192, or AES-256", AES128KeySize, AES192KeySize, AES256KeySize)
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
	paddedData := pkcs7Pad([]byte(b), aes.BlockSize)
	
	// Encrypt the data
	mode := cipher.NewCBCEncrypter(block, iv)
	encryptedData := make([]byte, len(paddedData))
	mode.CryptBlocks(encryptedData, paddedData)
	
	return encryptedData, nil
}

// Decrypt decrypts the blob data using AES-CBC
func (b Blob) Decrypt(key, iv []byte) ([]byte, error) {
	if len(key) != AES128KeySize && len(key) != AES192KeySize && len(key) != AES256KeySize {
		return nil, fmt.Errorf("key must be %d, %d, or %d bytes for AES-128, AES-192, or AES-256", AES128KeySize, AES192KeySize, AES256KeySize)
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
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padtext...)
}

// pkcs7Unpad removes PKCS7 padding from the data
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data is empty")
	}
	
	if len(data)%blockSize != 0 {
		return nil, fmt.Errorf("data is not padded correctly")
	}
	
	padding := int(data[len(data)-1])
	if padding > blockSize || padding == 0 {
		return nil, fmt.Errorf("invalid padding")
	}
	
	// Check if all padding bytes are correct
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	
	return data[:len(data)-padding], nil
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
