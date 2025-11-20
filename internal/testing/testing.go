package testing

import (
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestData(t *testing.T, filename string) []byte {
	// Get the file path of the caller (this file)
	_, curFilename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("Error: Could not get caller information")
	}

	// Get the directory of the file
	currentDir := filepath.Dir(curFilename)

	data, err := os.ReadFile(filepath.Join(currentDir, "testdata", filename))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func Unhex(t *testing.T, s string) []byte {
	r, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// GetFreePort returns an available port number for testing
// This function dynamically allocates ports to avoid conflicts during parallel test execution
func GetFreePort(t *testing.T) int {
	t.Helper()

	addr, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	defer addr.Close()

	return addr.Addr().(*net.TCPAddr).Port
}
