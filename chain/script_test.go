package chain

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lbryio/lbcd/txscript"
)

// Use a fixed test address (mainnet LBRY address format).
const testAddress = "bU269oqn4skRpj6J3uS96wJg6XDyTAfbvz"

func TestBuildP2PKHScript(t *testing.T) {
	params := LBRYParams()
	script, err := BuildP2PKHScript(testAddress, params)
	if err != nil {
		t.Fatalf("BuildP2PKHScript: %v", err)
	}
	// P2PKH = OP_DUP OP_HASH160 <push 20> <20 bytes> OP_EQUALVERIFY OP_CHECKSIG = 25 bytes
	if len(script) != 25 {
		t.Errorf("P2PKH script must be 25 bytes, got %d", len(script))
	}
	if script[0] != txscript.OP_DUP {
		t.Errorf("first byte = 0x%02x, want OP_DUP (0x76)", script[0])
	}
	if script[1] != txscript.OP_HASH160 {
		t.Errorf("second byte = 0x%02x, want OP_HASH160 (0xa9)", script[1])
	}
	if script[len(script)-1] != txscript.OP_CHECKSIG {
		t.Errorf("last byte = 0x%02x, want OP_CHECKSIG (0xac)", script[len(script)-1])
	}
}

func TestBuildClaimNameScript(t *testing.T) {
	params := LBRYParams()
	script, err := BuildClaimNameScript("test-name", []byte("claim value"), testAddress, params)
	if err != nil {
		t.Fatalf("BuildClaimNameScript: %v", err)
	}
	// Must start with OP_CLAIMNAME
	if script[0] != txscript.OP_CLAIMNAME {
		t.Errorf("first byte = 0x%02x, want OP_CLAIMNAME (0xb5)", script[0])
	}
	// Must end with OP_CHECKSIG (P2PKH suffix)
	if script[len(script)-1] != txscript.OP_CHECKSIG {
		t.Errorf("last byte = 0x%02x, want OP_CHECKSIG (0xac)", script[len(script)-1])
	}
	// Name must appear in the script (as a data push)
	if !strings.Contains(string(script), "test-name") {
		t.Error("script should contain the claim name")
	}
}

func TestBuildClaimNameScript_EmptyName(t *testing.T) {
	params := LBRYParams()
	_, err := BuildClaimNameScript("", []byte("value"), testAddress, params)
	if err == nil {
		t.Error("expected error for empty claim name")
	}
}

func TestBuildClaimNameScript_TooLong(t *testing.T) {
	params := LBRYParams()
	longName := strings.Repeat("a", MaxClaimNameSize+1)
	_, err := BuildClaimNameScript(longName, []byte("value"), testAddress, params)
	if err == nil {
		t.Error("expected error for claim name exceeding MaxClaimNameSize")
	}
}

func TestBuildUpdateClaimScript(t *testing.T) {
	params := LBRYParams()
	claimID := ClaimID{}
	for i := range claimID {
		claimID[i] = byte(i)
	}
	script, err := BuildUpdateClaimScript("test", claimID, []byte("value"), testAddress, params)
	if err != nil {
		t.Fatalf("BuildUpdateClaimScript: %v", err)
	}
	if script[0] != txscript.OP_UPDATECLAIM {
		t.Errorf("first byte = 0x%02x, want OP_UPDATECLAIM (0xb7)", script[0])
	}
	if script[len(script)-1] != txscript.OP_CHECKSIG {
		t.Errorf("last byte = 0x%02x, want OP_CHECKSIG (0xac)", script[len(script)-1])
	}
}

func TestBuildSupportClaimScript(t *testing.T) {
	params := LBRYParams()
	claimID := ClaimID{}
	for i := range claimID {
		claimID[i] = byte(i + 1)
	}
	// No value (most common case)
	script, err := BuildSupportClaimScript("test", claimID, nil, testAddress, params)
	if err != nil {
		t.Fatalf("BuildSupportClaimScript: %v", err)
	}
	if script[0] != txscript.OP_SUPPORTCLAIM {
		t.Errorf("first byte = 0x%02x, want OP_SUPPORTCLAIM (0xb6)", script[0])
	}
	if script[len(script)-1] != txscript.OP_CHECKSIG {
		t.Errorf("last byte = 0x%02x, want OP_CHECKSIG (0xac)", script[len(script)-1])
	}
}

func TestBuildSupportClaimScript_WithValue(t *testing.T) {
	params := LBRYParams()
	claimID := ClaimID{}
	script, err := BuildSupportClaimScript("test", claimID, []byte("comment"), testAddress, params)
	if err != nil {
		t.Fatalf("BuildSupportClaimScript: %v", err)
	}
	if script[0] != txscript.OP_SUPPORTCLAIM {
		t.Errorf("first byte = 0x%02x, want OP_SUPPORTCLAIM (0xb6)", script[0])
	}
}

func TestIsClaimScript(t *testing.T) {
	params := LBRYParams()
	claimScript, _ := BuildClaimNameScript("x", []byte("y"), testAddress, params)
	updateScript, _ := BuildUpdateClaimScript("x", ClaimID{}, []byte("y"), testAddress, params)
	supportScript, _ := BuildSupportClaimScript("x", ClaimID{}, nil, testAddress, params)
	p2pkhScript, _ := BuildP2PKHScript(testAddress, params)

	if !IsClaimScript(claimScript) {
		t.Error("claim_name script should be detected as claim")
	}
	if !IsClaimScript(updateScript) {
		t.Error("update_claim script should be detected as claim")
	}
	if !IsClaimScript(supportScript) {
		t.Error("support_claim script should be detected as claim")
	}
	if IsClaimScript(p2pkhScript) {
		t.Error("P2PKH script should NOT be detected as claim")
	}
	if IsClaimScript([]byte{}) {
		t.Error("empty script should NOT be detected as claim")
	}
}

func TestStripClaimScriptPrefix(t *testing.T) {
	params := LBRYParams()
	script, err := BuildClaimNameScript("test", []byte("value"), testAddress, params)
	if err != nil {
		t.Fatalf("BuildClaimNameScript: %v", err)
	}

	stripped := StripClaimScriptPrefix(script)
	// After stripping, what's left should be the P2PKH part (25 bytes)
	if len(stripped) != 25 {
		t.Errorf("stripped script must be 25 bytes (P2PKH), got %d", len(stripped))
	}
	if stripped[len(stripped)-1] != txscript.OP_CHECKSIG {
		t.Errorf("stripped script must end with OP_CHECKSIG")
	}

	// Round-trip: appending back should not give the same (since StripClaimScriptPrefix
	// returns what's left AFTER the claim prefix, including any OP_TRUE).
	// Actually, our scripts already have OP_TRUE stripped, so the stripped
	// part is exactly the P2PKH template.
	if hex.EncodeToString(stripped) != hex.EncodeToString(script[len(script)-25:]) {
		t.Errorf("stripped bytes don't match P2PKH suffix of original script")
	}
}

func TestBuildClaimNameScript_StripsOpTrue(t *testing.T) {
	// Regression for commit 44e03f1: ClaimNameScript must strip OP_TRUE
	// before appending P2PKH, otherwise the script contains an extra opcode.
	params := LBRYParams()
	script, err := BuildClaimNameScript("test", []byte("value"), testAddress, params)
	if err != nil {
		t.Fatalf("BuildClaimNameScript: %v", err)
	}

	// After OP_2DROP OP_DROP, the next byte should be OP_DUP (P2PKH),
	// not OP_TRUE (which would indicate we forgot to strip).
	for i := 0; i < len(script)-1; i++ {
		if script[i] == txscript.OP_2DROP && i+1 < len(script) && script[i+1] == txscript.OP_DROP {
			if i+2 < len(script) && script[i+2] == txscript.OP_TRUE {
				t.Errorf("OP_TRUE (0x51) found after OP_2DROP OP_DROP at offset %d — not stripped", i+2)
			}
			break
		}
	}
}

func TestMaxClaimConstants(t *testing.T) {
	if MaxClaimScriptSize != txscript.MaxClaimScriptSize {
		t.Errorf("MaxClaimScriptSize = %d, want %d", MaxClaimScriptSize, txscript.MaxClaimScriptSize)
	}
	if MaxClaimNameSize != txscript.MaxClaimNameSize {
		t.Errorf("MaxClaimNameSize = %d, want %d", MaxClaimNameSize, txscript.MaxClaimNameSize)
	}
}

func TestLBRYParams(t *testing.T) {
	p := LBRYParams()
	if p.PubKeyHashAddrID != 0x55 {
		t.Errorf("PubKeyHashAddrID = 0x%x, want 0x55", p.PubKeyHashAddrID)
	}
	if p.HDCoinType != SLIP44CoinType {
		t.Errorf("HDCoinType = %d, want %d", p.HDCoinType, SLIP44CoinType)
	}
}

// TestExtractClaimValue verifies round-trip extraction from claim scripts.
func TestExtractClaimValue(t *testing.T) {
	params := LBRYParams()

	// Normal claim value
	script, _ := BuildClaimNameScript("test", []byte("protobuf-value"), testAddress, params)
	val, err := ExtractClaimValue(script)
	if err != nil {
		t.Fatalf("ExtractClaimValue: %v", err)
	}
	if string(val) != "protobuf-value" {
		t.Errorf("want %q, got %q", "protobuf-value", string(val))
	}

	// Update claim
	claimID := ClaimID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14}
	script2, _ := BuildUpdateClaimScript("update", claimID, []byte("update-value"), testAddress, params)
	val2, err := ExtractClaimValue(script2)
	if err != nil {
		t.Fatalf("ExtractClaimValue update: %v", err)
	}
	if string(val2) != "update-value" {
		t.Errorf("want %q, got %q", "update-value", string(val2))
	}
}

// TestExtractClaimValue_MutationSafety verifies that mutating the returned
// slice does not corrupt the original script buffer.
func TestExtractClaimValue_MutationSafety(t *testing.T) {
	params := LBRYParams()
	script, _ := BuildClaimNameScript("test", []byte("mutate-me"), testAddress, params)
	original := make([]byte, len(script))
	copy(original, script)

	val, err := ExtractClaimValue(script)
	if err != nil {
		t.Fatalf("ExtractClaimValue: %v", err)
	}

	// Mutate the returned value
	for i := range val {
		val[i] = ^val[i]
	}

	// Script should be unchanged
	if string(script) != string(original) {
		t.Error("mutating ExtractClaimValue result corrupted the original script")
	}
}

// TestExtractClaimValue_SmallInt verifies that single-byte values in the
// 0x01-0x10 range are correctly extracted despite txscript.AddData encoding
// them as OP_1..OP_16 opcodes.
func TestExtractClaimValue_SmallInt(t *testing.T) {
	params := LBRYParams()
	for v := byte(0x01); v <= 0x10; v++ {
		script, _ := BuildClaimNameScript("test", []byte{v}, testAddress, params)
		val, err := ExtractClaimValue(script)
		if err != nil {
			t.Fatalf("ExtractClaimValue(0x%02x): %v", v, err)
		}
		if len(val) != 1 || val[0] != v {
			t.Errorf("0x%02x: want [%x], got %x", v, v, val)
		}
	}
}

// TestExtractClaimValue_LargePayload verifies OP_PUSHDATA2 handling for
// payloads exceeding 255 bytes.
func TestExtractClaimValue_LargePayload(t *testing.T) {
	params := LBRYParams()
	value := make([]byte, 256)
	for i := range value {
		value[i] = byte(i % 256)
	}

	script, _ := BuildClaimNameScript("test", value, testAddress, params)
	val, err := ExtractClaimValue(script)
	if err != nil {
		t.Fatalf("ExtractClaimValue: %v", err)
	}
	if string(val) != string(value) {
		t.Errorf("256-byte payload mismatch: len(val)=%d, len(value)=%d", len(val), len(value))
	}
}

// TestExtractClaimValue_MalformedPushData4 verifies that an OP_PUSHDATA4 with
// a length exceeding the remaining script bytes returns an error instead of
// panicking.
func TestExtractClaimValue_MalformedPushData4(t *testing.T) {
	// Craft a script: OP_CLAIMNAME followed by OP_PUSHDATA4 with a length
	// larger than the remaining bytes.
	script := []byte{
		txscript.OP_CLAIMNAME,
		txscript.OP_PUSHDATA4,
		0x00, 0x00, 0x00, 0x80, // length = 0x80000000 (2,147,483,648)
	}
	_, err := ExtractClaimValue(script)
	if err == nil {
		t.Error("expected error for malformed OP_PUSHDATA4, got nil")
	}
}

// TestExtractClaimValue_NegativeInt32 verifies that a length which wraps to
// negative on 32-bit int builds is rejected.
func TestExtractClaimValue_NegativeInt32(t *testing.T) {
	// Same as above: 0x80000000 converts to negative int on 32-bit
	script := []byte{
		txscript.OP_CLAIMNAME,
		txscript.OP_PUSHDATA4,
		0x00, 0x00, 0x00, 0x80,
	}
	_, err := ExtractClaimValue(script)
	if err == nil {
		t.Error("expected error for negative-wrapped length, got nil")
	}
	// Error message should mention the overflow
	if !strings.Contains(err.Error(), "exceeds script") && !strings.Contains(err.Error(), "length") {
		t.Errorf("expected length-related error, got: %v", err)
	}
}
