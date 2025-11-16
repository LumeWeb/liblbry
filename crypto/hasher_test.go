package crypto

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestSHA384Hasher_Hash(t *testing.T) {
	hasher := NewHasher()

	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "empty data",
			data:     []byte{},
			expected: "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
		},
		{
			name:     "hello world",
			data:     []byte("hello world"),
			expected: "fdbd8e75a67f29f701a4e040385e2e23986303ea10239211af907fcbb83578b3e417cb71ce646efd0819dd8c088de1bd",
		},
		{
			name:     "binary data",
			data:     []byte{0x00, 0x01, 0x02, 0x03, 0xFF},
			expected: "6454d81e5776ef01636cbe41c951f83361d016cc0feeb4801427460b94b4a2cb9143037a8510d1697a7b447e83f125b4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasher.Hash(tt.data)
			if result != tt.expected {
				t.Errorf("Hash() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestSHA384Hasher_HashReader(t *testing.T) {
	hasher := NewHasher()

	tests := []struct {
		name     string
		reader   io.Reader
		expected string
		hasError bool
	}{
		{
			name:     "string reader",
			reader:   strings.NewReader("hello world"),
			expected: "fdbd8e75a67f29f701a4e040385e2e23986303ea10239211af907fcbb83578b3e417cb71ce646efd0819dd8c088de1bd",
		},
		{
			name:     "bytes buffer",
			reader:   bytes.NewBufferString("hello world"),
			expected: "fdbd8e75a67f29f701a4e040385e2e23986303ea10239211af907fcbb83578b3e417cb71ce646efd0819dd8c088de1bd",
		},
		{
			name:     "empty reader",
			reader:   strings.NewReader(""),
			expected: "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
		},
		{
			name:     "large data",
			reader:   strings.NewReader(strings.Repeat("a", 10000)),
			expected: "2bca3b131bb7e922bcd1de98c44786d32e6b6b2993e69c4987edf9dd49711eb501f0e98ad248d839f6bf9e116e25a97c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := hasher.HashReader(tt.reader)
			if tt.hasError {
				if err == nil {
					t.Errorf("HashReader() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("HashReader() unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("HashReader() = %v, expected %v", result, tt.expected)
				}
			}
		})
	}
}

func TestSHA384Hasher_HashReader_ErrorHandling(t *testing.T) {
	hasher := NewHasher()

	// Test with a reader that always returns an error
	errorReader := &errorReader{}
	
	_, err := hasher.HashReader(errorReader)
	if err == nil {
		t.Error("HashReader() expected error but got none")
	}
}

func TestSHA384Hasher_Comparison(t *testing.T) {
	hasher := NewHasher()
	
	// Test that HashReader produces the same result as Hash for the same data
	testData := []byte("hello world")
	
	// Hash the data directly
	hashResult := hasher.Hash(testData)
	
	// Hash the data via reader
	readerResult, err := hasher.HashReader(bytes.NewReader(testData))
	if err != nil {
		t.Fatalf("HashReader() failed: %v", err)
	}
	
	// Compare results
	if hashResult != readerResult {
		t.Errorf("Hash() and HashReader() produced different results: %v vs %v", hashResult, readerResult)
	}
	
	// Test with empty data
	emptyData := []byte{}
	emptyHashResult := hasher.Hash(emptyData)
	emptyReaderResult, err := hasher.HashReader(bytes.NewReader(emptyData))
	if err != nil {
		t.Fatalf("HashReader() failed for empty data: %v", err)
	}
	
	if emptyHashResult != emptyReaderResult {
		t.Errorf("Hash() and HashReader() produced different results for empty data: %v vs %v", emptyHashResult, emptyReaderResult)
	}
}

func TestSHA384Hasher_IsValid(t *testing.T) {
	hasher := NewHasher()

	tests := []struct {
		name     string
		hash     string
		expected bool
	}{
		{
			name:     "valid hash",
			hash:     "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
			expected: true,
		},
		{
			name:     "invalid length - too short",
			hash:     "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b9",
			expected: false,
		},
		{
			name:     "invalid length - too long",
			hash:     "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b0",
			expected: false,
		},
		{
			name:     "invalid characters - uppercase",
			hash:     "38B060A751AC96384CD9327EB1B1E36A21FDB71114BE07434C0CC7BF63F6E1DA274EDEBFE76F65FBD51AD2F14898B95B",
			expected: false,
		},
		{
			name:     "invalid characters - non-hex",
			hash:     "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95g",
			expected: false,
		},
		{
			name:     "empty string",
			hash:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasher.IsValid(tt.hash)
			if result != tt.expected {
				t.Errorf("IsValid() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// Helper type for testing error conditions
type errorReader struct{}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}
