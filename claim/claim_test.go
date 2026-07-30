package claim

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/lbryio/lbcd/btcec"
	"github.com/lbryio/lbcd/chaincfg/chainhash"
	pb "go.lumeweb.com/liblbry/pb/v2"
)

var sdHashHex = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
var claimIDHex = "186f0042986695921100febae3f571ab9f369614"

func TestNewStream_CompilesValue(t *testing.T) {
	h, err := NewStream("My Video", "A test video", sdHashHex, "video/mp4", nil)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	if h.ClaimType != TypeStream {
		t.Errorf("ClaimType = %q, want stream", h.ClaimType)
	}
	if h.Version != NoSig {
		t.Errorf("Version = %d, want NoSig", h.Version)
	}

	value, err := h.CompileValue()
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	if len(value) == 0 {
		t.Fatal("CompileValue returned empty bytes")
	}
	if value[0] != byte(NoSig) {
		t.Errorf("first byte = %d, want %d", value[0], NoSig)
	}

	// Verify protobuf round-trip
	var claim pb.Claim
	if err := claim.UnmarshalVT(value[1:]); err != nil {
		t.Fatalf("unmarshal claim: %v", err)
	}
	if claim.Title != "My Video" {
		t.Errorf("title = %q", claim.Title)
	}
	if claim.GetStream() == nil {
		t.Fatal("claim has no stream")
	}
	if claim.GetStream().GetSource().GetMediaType() != "video/mp4" {
		t.Errorf("media type = %q", claim.GetStream().GetSource().GetMediaType())
	}
}

func TestNewStream_WithChannelClaimID(t *testing.T) {
	channelID, _ := hex.DecodeString(claimIDHex)
	h, err := NewStream("Signed Video", "", sdHashHex, "video/mp4", channelID)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	// Version is not upgraded until SignStream runs.
	if h.Version != NoSig {
		t.Errorf("Version = %d, want NoSig before signing", h.Version)
	}
	if !bytes.Equal(h.ClaimID, channelID) {
		t.Errorf("ClaimID mismatch")
	}

	// Version stays NoSig until SignStream runs, so CompileValue succeeds
	// without a signature (it compiles as unsigned).
	_, err = h.CompileValue()
	if err != nil {
		t.Fatalf("CompileValue should succeed before signing (unsigned): %v", err)
	}

	value, err := signedStreamValue(t, h, channelID)
	if err != nil {
		t.Fatalf("sign and compile: %v", err)
	}
	if len(value) < 1+20 {
		t.Fatalf("signed value too short: %d bytes", len(value))
	}
	if value[0] != byte(WithSig) {
		t.Errorf("first byte = %d, want %d", value[0], WithSig)
	}
	if !bytes.Equal(value[1:21], channelID) {
		t.Errorf("compiled ClaimID mismatch")
	}
}

func TestNewStream_InvalidSDHash(t *testing.T) {
	_, err := NewStream("x", "", "not hex", "video/mp4", nil)
	if err == nil {
		t.Error("expected error for invalid sdhash hex")
	}
	_, err = NewStream("x", "", "abcd", "video/mp4", nil) // too short
	if err == nil {
		t.Error("expected error for short sdhash")
	}
}

func TestNewChannel_CompilesValue(t *testing.T) {
	privKey, pubKey := btcec.PrivKeyFromBytes(btcec.S256(), []byte{1})
	_ = privKey
	pubKeyBytes := pubKey.SerializeCompressed()
	if len(pubKeyBytes) != 33 {
		t.Fatalf("expected 33-byte compressed pubkey, got %d", len(pubKeyBytes))
	}

	h, err := NewChannel("My Channel", pubKeyBytes)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	if h.ClaimType != TypeChannel {
		t.Errorf("ClaimType = %q", h.ClaimType)
	}
	value, err := h.CompileValue()
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}

	var claim pb.Claim
	if err := claim.UnmarshalVT(value[1:]); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if claim.Title != "My Channel" {
		t.Errorf("title = %q", claim.Title)
	}
	if claim.GetChannel() == nil {
		t.Fatal("claim has no channel")
	}
	if !bytes.Equal(claim.GetChannel().GetPublicKey(), pubKeyBytes) {
		t.Error("channel public key mismatch")
	}
}

func TestNewChannel_InvalidPublicKey(t *testing.T) {
	// nil public key
	_, err := NewChannel("x", nil)
	if err == nil {
		t.Error("expected error for nil public key")
	}
	// 31-byte public key
	_, err = NewChannel("x", []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	if err == nil {
		t.Error("expected error for 31-byte public key")
	}
	// 34-byte public key
	_, err = NewChannel("x", []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	if err == nil {
		t.Error("expected error for 34-byte public key")
	}
}

func TestNewRepost_CompilesValue(t *testing.T) {
	h, err := NewRepost("Repost", claimIDHex)
	if err != nil {
		t.Fatalf("NewRepost: %v", err)
	}
	if h.ClaimType != TypeRepost {
		t.Errorf("ClaimType = %q", h.ClaimType)
	}

	value, err := h.CompileValue()
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}

	var claim pb.Claim
	if err := claim.UnmarshalVT(value[1:]); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if claim.GetRepost() == nil {
		t.Fatal("claim has no repost")
	}
	wantID, _ := hex.DecodeString(claimIDHex)
	if !bytes.Equal(claim.GetRepost().GetClaimHash(), wantID) {
		t.Errorf("repost claim hash mismatch: %x vs %x", claim.GetRepost().GetClaimHash(), wantID)
	}
}

func TestNewSupport_Serializes(t *testing.T) {
	// An empty emoji serializes to empty bytes in proto3 (all-zero message),
	// which is valid. A non-empty emoji must round-trip.
	s := NewSupport("🔥")
	b, err := SupportValue(s)
	if err != nil {
		t.Fatalf("SupportValue: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("SupportValue for non-empty emoji returned empty bytes")
	}
	var roundtrip pb.Support
	if err := roundtrip.UnmarshalVT(b); err != nil {
		t.Fatalf("unmarshal support: %v", err)
	}
	if roundtrip.Emoji != "🔥" {
		t.Errorf("emoji = %q", roundtrip.Emoji)
	}
}

func TestSignStream_Produces64ByteSignature(t *testing.T) {
	channelPrivKey, channelPubKey := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})

	channelID, _ := hex.DecodeString(claimIDHex)
	h, err := NewStream("Signed", "", sdHashHex, "video/mp4", channelID)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}

	// firstInputTxID is arbitrary for the test
	firstInputTxID := "0000000000000000000000000000000000000000000000000000000000000001"
	if err := SignStream(h, channelPrivKey, firstInputTxID, channelID); err != nil {
		t.Fatalf("SignStream: %v", err)
	}

	if h.Version != WithSig {
		t.Errorf("Version = %d, want WithSig", h.Version)
	}
	if len(h.Signature) != 64 {
		t.Errorf("signature length = %d, want 64", len(h.Signature))
	}

	// Verify compiled value contains version + claimID + signature + payload
	value, err := h.CompileValue()
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	if len(value) != 1+20+64+len(mustMarshal(h.Claim)) {
		t.Errorf("compiled value length = %d, want %d", len(value), 1+20+64+len(mustMarshal(h.Claim)))
	}

	// Manually verify the digest and signature.
	// Use chainhash.NewHashFromStr to match SignStream's internal parsing.
	txidHash, err := chainhash.NewHashFromStr(firstInputTxID)
	if err != nil {
		t.Fatalf("parse txid: %v", err)
	}
	payload := mustMarshal(h.Claim)
	digest := append(txidHash[:], channelID...)
	digest = append(digest, payload...)
	hash := sha256.Sum256(digest)

	// Reconstruct btcec.Signature from the compact R||S bytes stored in
	// h.Signature and verify directly against the public key.
	compactSig, err := ParseCompactSignature(h.Signature)
	if err != nil {
		t.Fatalf("parse compact signature: %v", err)
	}
	if !compactSig.Verify(hash[:], channelPubKey) {
		t.Error("compact signature does not verify")
	}
}

func signedStreamValue(t *testing.T, h *Helper, channelID []byte) ([]byte, error) {
	t.Helper()
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})
	firstInputTxID := "0000000000000000000000000000000000000000000000000000000000000001"
	if err := SignStream(h, privKey, firstInputTxID, channelID); err != nil {
		return nil, err
	}
	return h.CompileValue()
}

func TestSignStream_RequiresStreamClaim(t *testing.T) {
	channelPrivKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})
	channel, err := NewChannel("x", []byte{1})
	if err == nil {
		t.Error("expected error for invalid public key length")
	}
	// channel is nil on error; create a valid channel for the non-stream test.
	channel, _ = NewChannel("x", []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	err = SignStream(channel, channelPrivKey, "0000000000000000000000000000000000000000000000000000000000000001", nil)
	if err == nil {
		t.Error("expected error signing non-stream claim")
	}
}

func TestSignStream_InvalidChannelClaimIDLength(t *testing.T) {
	channelPrivKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})
	channelID, _ := hex.DecodeString(claimIDHex)
	h, err := NewStream("Signed", "", sdHashHex, "video/mp4", channelID)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	// 19 bytes is not 20 — should be rejected.
	badID := make([]byte, 19)
	copy(badID, channelID)
	err = SignStream(h, channelPrivKey, "0000000000000000000000000000000000000000000000000000000000000001", badID)
	if err == nil {
		t.Error("expected error for channel claim ID not 20 bytes")
	}
}

func TestSignStream_InvalidTxIDLength(t *testing.T) {
	channelPrivKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})
	channelID, _ := hex.DecodeString(claimIDHex)
	h, err := NewStream("Signed", "", sdHashHex, "video/mp4", channelID)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	err = SignStream(h, channelPrivKey, "00000000000000000000000000000000000000000000000000000000000001", channelID)
	if err == nil {
		t.Error("expected error for short txid")
	}
}

func TestSignStream_InvalidTxIDHex(t *testing.T) {
	channelPrivKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})
	channelID, _ := hex.DecodeString(claimIDHex)
	h, err := NewStream("Signed", "", sdHashHex, "video/mp4", channelID)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	err = SignStream(h, channelPrivKey, "not hex", channelID)
	if err == nil {
		t.Error("expected error for invalid txid hex")
	}
}

func TestSignStream_MismatchedChannelClaimID(t *testing.T) {
	channelPrivKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})
	channelID, _ := hex.DecodeString(claimIDHex)
	h, err := NewStream("Signed", "", sdHashHex, "video/mp4", channelID)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	// h.ClaimID is already channelID from NewStream.
	// Signing with a different channel ID should error.
	otherID := make([]byte, 20)
	copy(otherID, channelID)
	otherID[19] ^= 0xFF // flip last byte to make it different
	err = SignStream(h, channelPrivKey, "0000000000000000000000000000000000000000000000000000000000000001", otherID)
	if err == nil {
		t.Error("expected error for mismatched channel claim ID")
	}
}

func TestSignStream_SameChannelClaimIDOK(t *testing.T) {
	channelPrivKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})
	channelID, _ := hex.DecodeString(claimIDHex)
	h, err := NewStream("Signed", "", sdHashHex, "video/mp4", channelID)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	// Signing with the same channel ID as NewStream stored should succeed.
	err = SignStream(h, channelPrivKey, "0000000000000000000000000000000000000000000000000000000000000001", channelID)
	if err != nil {
		t.Fatalf("SignStream with matching channel ID: %v", err)
	}
	if h.Version != WithSig {
		t.Errorf("Version = %d, want WithSig", h.Version)
	}
}

func TestCompactSignature_Padding(t *testing.T) {
	// Create a tiny signature to test zero-padding behavior.
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{3})
	hash := sha256.Sum256([]byte("test"))
	sig, err := privKey.Sign(hash[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	compact := CompactSignature(sig)
	if len(compact) != 64 {
		t.Errorf("CompactSignature length = %d", len(compact))
	}
}

func TestCompactSignature_LowSNormalization(t *testing.T) {
	// Manually construct a signature with S > N/2 (high-S)
	// and verify CompactSignature normalizes it to low-S.
	curve := btcec.S256()
	r := new(big.Int).SetInt64(12345)
	sHigh := new(big.Int).Add(curve.N, new(big.Int).Neg(new(big.Int).Rsh(curve.N, 1)))
	sHigh.Sub(curve.N, new(big.Int).SetInt64(1)) // S = N - 1, definitely > N/2

	sig := &btcec.Signature{R: r, S: sHigh}
	compact := CompactSignature(sig)

	// Reconstruct and verify S is now <= N/2
	parsed, err := ParseCompactSignature(compact)
	if err != nil {
		t.Fatalf("parse compact: %v", err)
	}
	halfOrder := new(big.Int).Rsh(curve.N, 1)
	if parsed.S.Cmp(halfOrder) != -1 && parsed.S.Cmp(halfOrder) != 0 {
		t.Errorf("S not normalized: %s > %s", parsed.S.Text(16), halfOrder.Text(16))
	}

	// Verify R is unchanged
	if parsed.R.Cmp(r) != 0 {
		t.Error("R was modified during low-S normalization")
	}
}

func TestParseClaimValue_UnsignedRoundTrip(t *testing.T) {
	h, err := NewStream("RoundTrip", "desc", sdHashHex, "video/mp4", nil)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	compiled, err := h.CompileValue()
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}

	parsed, err := ParseClaimValue(compiled)
	if err != nil {
		t.Fatalf("ParseClaimValue: %v", err)
	}
	if parsed.Version != NoSig {
		t.Errorf("Version = %d, want NoSig", parsed.Version)
	}
	if parsed.Claim.GetTitle() != "RoundTrip" {
		t.Errorf("Title = %q", parsed.Claim.GetTitle())
	}
	if parsed.Claim.GetStream() == nil {
		t.Error("parsed claim has no stream")
	}
}

func TestParseClaimValue_SignedRoundTrip(t *testing.T) {
	channelPrivKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})
	channelID, _ := hex.DecodeString(claimIDHex)
	h, err := NewStream("Signed RT", "", sdHashHex, "video/mp4", channelID)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	firstInputTxID := "0000000000000000000000000000000000000000000000000000000000000001"
	if err := SignStream(h, channelPrivKey, firstInputTxID, channelID); err != nil {
		t.Fatalf("SignStream: %v", err)
	}

	compiled, err := h.CompileValue()
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	if len(compiled) < 1+20+64 {
		t.Fatalf("compiled value too short: %d", len(compiled))
	}

	parsed, err := ParseClaimValue(compiled)
	if err != nil {
		t.Fatalf("ParseClaimValue: %v", err)
	}
	if parsed.Version != WithSig {
		t.Errorf("Version = %d, want WithSig", parsed.Version)
	}
	if !bytes.Equal(parsed.ClaimID, channelID) {
		t.Errorf("ClaimID mismatch: %x vs %x", parsed.ClaimID, channelID)
	}
	if len(parsed.Signature) != 64 {
		t.Errorf("Signature len = %d, want 64", len(parsed.Signature))
	}
	if parsed.Claim.GetTitle() != "Signed RT" {
		t.Errorf("Title = %q", parsed.Claim.GetTitle())
	}
}

func TestParseClaimValue_InvalidData(t *testing.T) {
	// Empty data
	_, err := ParseClaimValue([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
	// Unsupported version (2)
	_, err = ParseClaimValue([]byte{2, 0, 0})
	if err == nil {
		t.Error("expected error for unsupported version")
	}
	// Signed value too short
	_, err = ParseClaimValue([]byte{1, 0, 0})
	if err == nil {
		t.Error("expected error for short signed value")
	}
}

func TestNewCollection_CompilesValue(t *testing.T) {
	h, err := NewCollection("My Playlist", []string{
		"186f0042986695921100febae3f571ab9f369614",
		"186f0042986695921100febae3f571ab9f369615",
	})
	if err != nil {
		t.Fatalf("NewCollection: %v", err)
	}
	if h.ClaimType != TypeCollection {
		t.Errorf("ClaimType = %q, want collection", h.ClaimType)
	}

	value, err := h.CompileValue()
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}
	var claim pb.Claim
	if err := claim.UnmarshalVT(value[1:]); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if claim.GetCollection() == nil {
		t.Fatal("claim has no collection")
	}
	if len(claim.GetCollection().GetClaimReferences()) != 2 {
		t.Errorf("claim references = %d, want 2", len(claim.GetCollection().GetClaimReferences()))
	}
}

func TestNewCollection_InvalidClaimID(t *testing.T) {
	_, err := NewCollection("Bad", []string{"not-a-hex-id"})
	if err == nil {
		t.Error("expected error for invalid claim ID")
	}
}

func TestParseClaimValue_SliceAliasing(t *testing.T) {
	channelPrivKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), []byte{2})
	channelID, _ := hex.DecodeString(claimIDHex)
	h, err := NewStream("Aliasing", "", sdHashHex, "video/mp4", channelID)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	firstInputTxID := "0000000000000000000000000000000000000000000000000000000000000001"
	if err := SignStream(h, channelPrivKey, firstInputTxID, channelID); err != nil {
		t.Fatalf("SignStream: %v", err)
	}

	compiled, err := h.CompileValue()
	if err != nil {
		t.Fatalf("CompileValue: %v", err)
	}

	parsed, err := ParseClaimValue(compiled)
	if err != nil {
		t.Fatalf("ParseClaimValue: %v", err)
	}
	if !bytes.Equal(parsed.ClaimID, channelID) || len(parsed.Signature) != 64 {
		t.Fatal("parsed values invalid before mutation")
	}

	// Mutate the original buffer — parsed slices must remain independent.
	for i := range compiled {
		compiled[i] = 0xFF
	}
	if !bytes.Equal(parsed.ClaimID, channelID) {
		t.Error("ClaimID was corrupted by buffer reuse (slice aliasing)")
	}
	for _, b := range parsed.Signature {
		if b == 0xFF {
			t.Error("Signature was corrupted by buffer reuse (slice aliasing)")
			break
		}
	}
}

func TestSignStream_CrossImplementationVector(t *testing.T) {
	// Deterministic scalar = 1, matching Python coincurve with b'\x00'*31 + b'\x01'.
	scalar := make([]byte, 32)
	scalar[31] = 1
	privKey, pubKey := btcec.PrivKeyFromBytes(btcec.S256(), scalar)

	// Build identical stream claim as Python reference.
	sdHash, _ := hex.DecodeString("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	claim := &pb.Claim{
		Title:       "Vector",
		Description: "",
		Type: &pb.Claim_Stream{
			Stream: &pb.Stream{
				Source: &pb.Source{
					SdHash:    sdHash,
					MediaType: "video/mp4",
				},
			},
		},
	}
	payload, _ := claim.MarshalVT()

	// Inputs (display-order txid, 20-byte claim ID).
	txidHash, _ := chainhash.NewHashFromStr("0000000000000000000000000000000000000000000000000000000000000001")
	claimID, _ := hex.DecodeString("186f0042986695921100febae3f571ab9f369614")

	// Compute digest matching Python reference.
	digest := append(txidHash[:], claimID...)
	digest = append(digest, payload...)

	// Expected digest from Python reference.
	wantDigest := "88fd2fc61b9e2e7f9a40c0d04ec8d391554b17d77475aea6eabaff97255bee03"
	gotDigest := sha256.Sum256(digest)
	if hex.EncodeToString(gotDigest[:]) != wantDigest {
		t.Fatalf("digest mismatch:\nwant %s\ngot  %x", wantDigest, gotDigest[:])
	}

	// Sign and verify.
	sig, err := privKey.Sign(gotDigest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !sig.Verify(gotDigest[:], pubKey) {
		t.Fatal("signature does not verify")
	}

	// Expected compact signature from Python reference.
	wantCompact := "7a0a426011c6bdaa086d2902acafcac54d705414825123e8f07df90df5b2a6f267c0e645ee92b6f7bc822fe768687c208b6aa86d1e2341a66d6d516f33371123"
	compact := CompactSignature(sig)
	if hex.EncodeToString(compact) != wantCompact {
		t.Fatalf("compact sig mismatch:\nwant %s\ngot  %x", wantCompact, compact)
	}
}

func mustMarshal(m *pb.Claim) []byte {
	b, _ := m.MarshalVT()
	return b
}
