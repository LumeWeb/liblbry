package testing

import (
	"encoding/hex"
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
