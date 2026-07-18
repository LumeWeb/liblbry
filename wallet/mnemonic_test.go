package wallet

import (
	"strings"
	"testing"
)

func TestMakeSeed(t *testing.T) {
	seed, err := MakeSeed()
	if err != nil {
		t.Fatalf("MakeSeed: %v", err)
	}
	words := strings.Fields(seed)
	if len(words) != 12 {
		t.Errorf("expected 12 words, got %d: %s", len(words), seed)
	}
	if !IsNewSeed(seed, SeedPrefix) {
		t.Errorf("seed does not pass IsNewSeed: %s", seed)
	}
}

func TestMnemonicDecode(t *testing.T) {
	seed, err := MakeSeed()
	if err != nil {
		t.Fatalf("MakeSeed: %v", err)
	}
	decoded, err := MnemonicDecode(seed)
	if err != nil {
		t.Fatalf("MnemonicDecode: %v", err)
	}
	reencoded := MnemonicEncode(decoded)
	if reencoded != seed {
		t.Errorf("round-trip mismatch:\n  original:  %s\n  reencoded: %s", seed, reencoded)
	}
}

func TestIsValid(t *testing.T) {
	seed, err := MakeSeed()
	if err != nil {
		t.Fatalf("MakeSeed: %v", err)
	}
	if !IsValid(seed) {
		t.Errorf("IsValid returned false for generated seed: %s", seed)
	}
}

func TestIsValid_InvalidWord(t *testing.T) {
	if IsValid("abandon ability able about above absent absorb abstract absurd abuse access notaword") {
		t.Error("IsValid should return false for invalid word")
	}
}

func TestIsValid_Empty(t *testing.T) {
	if IsValid("") {
		t.Error("IsValid should return false for empty string")
	}
}

func TestIsValid_TestMnemonic(t *testing.T) {
	if !IsValid(TestMnemonic) {
		t.Errorf("TestMnemonic %q should be valid", TestMnemonic)
	}
}

func TestNormalizeText(t *testing.T) {
	got := NormalizeText("ABANDON ABILITY")
	want := "abandon ability"
	if got != want {
		t.Errorf("NormalizeText: got %q, want %q", got, want)
	}
}

func TestMnemonicToSeed(t *testing.T) {
	seed := "abandon ability able about above absent absorb abstract absurd abuse access accident"
	result1 := MnemonicToSeed(seed, "lbryum")
	result2 := MnemonicToSeed(seed, "lbryum")
	if len(result1) != 64 {
		t.Errorf("expected 64 bytes, got %d", len(result1))
	}
	if string(result1) != string(result2) {
		t.Error("MnemonicToSeed not deterministic")
	}
}
