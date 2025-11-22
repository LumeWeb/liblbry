package testing

// Test vectors and constants shared across all hash-related tests

// Test data keys - use these constants to avoid fragile string literals
const (
	TestKeyEmpty      = "empty"
	TestKeyHelloWorld = "hello_world"
	TestKeyLbry       = "lbry"
	TestKeyTestData   = "test_data"

	ValidHashKeyEmptyString = "empty_string"
	ValidHashKeyHelloWorld  = "hello_world"
	ValidHashKeyLbry        = "lbry"
	ValidHashKeyBlob        = "blob"
	ValidHashKeyStream      = "stream"
	ValidHashKeyBlobInfo    = "blob_info"
	ValidHashKeyNullStream  = "null_stream"

	InvalidLengthKeyEmpty       = "empty"
	InvalidLengthKeyTooShort    = "too_short"
	InvalidLengthKeyWrongLength = "wrong_length"

	InvalidHexKeyInvalidG = "invalid_g"
	InvalidHexKeyInvalidZ = "invalid_z"
	InvalidHexKeyMixed    = "mixed"

	InvalidUppercaseKeyAllUppercase = "all_uppercase"
	InvalidUppercaseKeyMixedCase    = "mixed_case"

	InvalidMultihashKeyInvalid = "invalid"
	InvalidMultihashKeyEmpty   = "empty"
	InvalidMultihashKeyCIDv0   = "cidv0"

	// LBRYTestHashes keys
	LBRYHashKey1 = "hash1"
	LBRYHashKey2 = "hash2"
	LBRYHashKey3 = "hash3"
	LBRYHashKey4 = "hash4"
	LBRYHashKey5 = "hash5"
	LBRYHashKey6 = "hash6"
)

// LBRYTestHashes contains real LBRY blob hashes from lbry.go test fixtures
var LBRYTestHashes = map[string]string{
	LBRYHashKey1: "a2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e6c",
	LBRYHashKey2: "0c9675ad7f40f29dcd41883ed9cf7e145bbb13976d9b83ab9354f4f61a87f0f7771a56724c2aa7a5ab43c68d7942e5cb",
	LBRYHashKey3: "a4d07d442b9907036c75b6c92db316a8b8428733bf5ec976627a48a7c862bf84db33075d54125a7c0b297bd2dc445f1c",
	LBRYHashKey4: "dcd2093f4a3eca9f6dd59d785d0bef068fee788481986aa894cf72ed4d992c0ff9d19d1743525de2f5c3c62f5ede1c58",
	LBRYHashKey5: "2c8cb2893668ef3ad30bda5b3361c0736d746d82fb16155d1510c4d2c5e4481d49ee747f155b2f1156849d422f13a7be",
	LBRYHashKey6: "4d9a9ce3d72af9f171c4233738e08440937cf906eb506a5d573c0e5500c58500b0a6cbaedc9be2c863750859c01d9954",
}

// KnownHashVectors contains known Input/output pairs for SHA-384 hashing
var KnownHashVectors = map[string]struct {
	Input    string
	Expected string
}{
	TestKeyEmpty: {
		Input:    "",
		Expected: "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
	},
	TestKeyHelloWorld: {
		Input:    "hello world",
		Expected: "fdbd8e75a67f29f701a4e040385e2e23986303ea10239211af907fcbb83578b3e417cb71ce646efd0819dd8c088de1bd",
	},
	TestKeyLbry: {
		Input:    "lbry",
		Expected: "eb5b19503c4ccfded3b7ecce42faefa088ef523ce929d802aa57e25fe50a543477fa90238fa06897a4aa9c6376c77440",
	},
	TestKeyTestData: {
		Input:    "test data for hashing",
		Expected: "86b862d2876ad1b8411fcf772ecd315f18c28580eaf664aed9012578b3a4e977cebe16b986ac37f0adb55ca96c53c165",
	},
}

// ValidLBRYHashes includes all valid LBRY hashes for testing validation
var ValidLBRYHashes = map[string]string{
	ValidHashKeyEmptyString: "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b", // empty string hash
	ValidHashKeyHelloWorld:  "fdbd8e75a67f29f701a4e040385e2e23986303ea10239211af907fcbb83578b3e417cb71ce646efd0819dd8c088de1bd", // "hello world" hash
	ValidHashKeyLbry:        "8305781102803a0f04c143260a09d647e81004a2d58b5136784110b4499278151f0a5e024ff375a1e4b60598e30bd91b", // "lbry" hash
	ValidHashKeyBlob:        "a2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e6c", // LBRY blob hash
	ValidHashKeyStream:      "d756e860d8f49d03937c1a35a560636e792d97bee6f9660fc69e206cbcfe7f9297c5ede8428dd5f17d240e434eb557da", // LBRY stream hash
	ValidHashKeyBlobInfo:    "2c8cb2893668ef3ad30bda5b3361c0736d746d82fb16155d1510c4d2c5e4481d49ee747f155b2f1156849d422f13a7be", // LBRY blob info hash
	ValidHashKeyNullStream:  "4d9a9ce3d72af9f171c4233738e08440937cf906eb506a5d573c0e5500c58500b0a6cbaedc9be2c863750859c01d9954", // LBRY null stream hash
}

// InvalidHashLengths contains hashes with invalid lengths for testing
var InvalidHashLengths = map[string]string{
	InvalidLengthKeyEmpty:       "",                                                                                               // empty
	InvalidLengthKeyTooShort:    "abc",                                                                                            // too short
	InvalidLengthKeyWrongLength: "3838383838383838383838383838383838383838383838383838383838383838383838383838383838383838383838", // wrong length
}

// InvalidHashHex contains hashes with invalid hex characters for testing
var InvalidHashHex = map[string]string{
	InvalidHexKeyInvalidG: "38383838383838383838383838383838383838383838383838383838383838383838383838383838383838383838383g",     // invalid hex character
	InvalidHexKeyInvalidZ: "38383838383838383838383838383838383838383838383838383838383838383838383838383838383838383838383z",     // invalid hex character
	InvalidHexKeyMixed:    "abc" + "38383838383838383838383838383838383838383838383838383838383838383838383838383838383838383838", // mixed valid/invalid
}

// InvalidHashUppercase contains hashes with uppercase characters for testing
var InvalidHashUppercase = map[string]string{
	InvalidUppercaseKeyAllUppercase: "38383838383838383838383838383838383838383838383838383838383838383838383838383838383838383838383A",    // uppercase
	InvalidUppercaseKeyMixedCase:    "ABCDEF" + "3838383838383838383838383838383838383838383838383838383838383838383838383838383838383838", // mixed case
}

// InvalidMultihashes contains invalid multihash strings for testing
var InvalidMultihashes = map[string]string{
	InvalidMultihashKeyInvalid: "invalid",
	InvalidMultihashKeyEmpty:   "",
	InvalidMultihashKeyCIDv0:   "QmUnvqVuUc5ZeeTYrXHF3HEtVvaYH4U1qf5KXqVQ4yRL2k", // CIDv0 (incompatible with SHA-384)
}

// TestDataForHashing contains various test data strings for comprehensive testing
var TestDataForHashing = []string{
	"",
	"hello",
	"hello world",
	"lbry",
	"test data for hashing",
	"This is a longer string to test with more complex data that should still produce a valid hash",
}

// Helper function to get LBRY test hash keys in order
func GetLBRYTestHashKeys() []string {
	return []string{LBRYHashKey1, LBRYHashKey2, LBRYHashKey3, LBRYHashKey4, LBRYHashKey5, LBRYHashKey6}
}
