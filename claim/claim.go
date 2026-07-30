// Package claim builds LBRY protobuf claims (channel, stream, repost,
// support) and compiles them into the value bytes pushed in claim scripts.
//
// The protobuf types come from go.lumeweb.com/liblbry/pb/v2 (generated
// with protoc-gen-go-lite — reflection-free, TinyGo compatible).
//
// Serialization and signing logic is adapted from lbry.go/schema/stake
// (MIT License, LBRY Inc), modified to use lbcd/btcec instead of the
// old lbryio/lbrycrd.go fork.
package claim

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/lbryio/lbcd/btcec"
	"github.com/lbryio/lbcd/chaincfg/chainhash"
	pb "go.lumeweb.com/liblbry/pb/v2"
)

// Version encodes the claim value version byte (0 = unsigned, 1 = signed).
type Version byte

const (
	// NoSig (version 0): unsigned claim value.
	NoSig Version = 0
	// WithSig (version 1): claim signed by a channel.
	WithSig Version = 1
)

// ClaimType identifies the kind of claim contained in a Helper.
type ClaimType string

const (
	TypeStream     ClaimType = "stream"
	TypeChannel    ClaimType = "channel"
	TypeRepost     ClaimType = "repost"
	TypeCollection ClaimType = "collection"
)

// Helper holds a protobuf claim and optional signature data.
// It mirrors lbry.go's StakeHelper but uses lbcd's btcec.
type Helper struct {
	Claim     *pb.Claim
	ClaimType ClaimType
	ClaimID   []byte
	Version   Version
	Signature []byte
}

// NewChannel creates a Helper for a channel claim.
// publicKey must be the compressed secp256k1 public key (33 bytes).
func NewChannel(title string, publicKey []byte) (*Helper, error) {
	if len(publicKey) != 33 {
		return nil, fmt.Errorf("publicKey must be 33 bytes (compressed secp256k1), got %d", len(publicKey))
	}
	return &Helper{
		ClaimType: TypeChannel,
		Claim: &pb.Claim{
			Title: title,
			Type: &pb.Claim_Channel{
				Channel: &pb.Channel{
					PublicKey: publicKey,
				},
			},
		},
		Version: NoSig,
	}, nil
}

// NewStream creates a Helper for a stream claim.
// sdHash is the hex stream descriptor hash. mediaType is the MIME type.
// If channelClaimID is non-nil, the stream will be configured for
// channel signing (Version remains NoSig until SignStream attaches
// the signature).
func NewStream(title, description, sdHash, mediaType string, channelClaimID []byte) (*Helper, error) {
	sdHashBytes, err := hex.DecodeString(sdHash)
	if err != nil {
		return nil, fmt.Errorf("decode sdhash: %w", err)
	}
	if len(sdHashBytes) != 48 {
		return nil, fmt.Errorf("sdhash must be 48 bytes (SHA-384), got %d", len(sdHashBytes))
	}

	helper := &Helper{
		ClaimType: TypeStream,
		Claim: &pb.Claim{
			Title:       title,
			Description: description,
			Type: &pb.Claim_Stream{
				Stream: &pb.Stream{
					Source: &pb.Source{
						SdHash:    sdHashBytes,
						MediaType: mediaType,
					},
				},
			},
		},
		Version: NoSig,
		ClaimID: channelClaimID,
	}

	return helper, nil
}

// NewCollection creates a Helper for a collection claim.
// claimIDHexes are the 40-character hex claim IDs in the collection.
func NewCollection(title string, claimIDHexes []string) (*Helper, error) {
	refs := make([]*pb.ClaimReference, len(claimIDHexes))
	for i, id := range claimIDHexes {
		claimID, err := normalizeClaimID(id)
		if err != nil {
			return nil, fmt.Errorf("claim %d: %w", i, err)
		}
		refs[i] = &pb.ClaimReference{ClaimHash: claimID}
	}
	return &Helper{
		ClaimType: TypeCollection,
		Claim: &pb.Claim{
			Title: title,
			Type: &pb.Claim_Collection{
				Collection: &pb.ClaimList{
					ListType:        pb.ClaimList_COLLECTION,
					ClaimReferences: refs,
				},
			},
		},
		Version: NoSig,
	}, nil
}

// NewRepost creates a Helper for a repost claim.
// claimIDHex is the 40-character hex claim ID being reposted.
func NewRepost(title string, claimIDHex string) (*Helper, error) {
	claimID, err := normalizeClaimID(claimIDHex)
	if err != nil {
		return nil, err
	}
	return &Helper{
		ClaimType: TypeRepost,
		Claim: &pb.Claim{
			Title: title,
			Type: &pb.Claim_Repost{
				Repost: &pb.ClaimReference{
					ClaimHash: claimID,
				},
			},
		},
		Version: NoSig,
	}, nil
}

// NewSupport creates the protobuf payload for a support claim.
// Support transactions include an optional emoji as the value; the claim
// being supported is identified by the transaction's claim script, not
// the protobuf payload.
func NewSupport(emoji string) *pb.Support {
	return &pb.Support{Emoji: emoji}
}

// normalizeClaimID accepts a 40-char hex claim ID and returns 20 raw bytes.
func normalizeClaimID(id string) ([]byte, error) {
	s := strings.TrimPrefix(id, "0x")
	if len(s) != 40 {
		return nil, fmt.Errorf("claim ID must be 40 hex characters, got %d", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode claim ID: %w", err)
	}
	if len(b) != 20 {
		return nil, fmt.Errorf("claim ID must be 20 bytes, got %d", len(b))
	}
	return b, nil
}

// CompileValue serializes the Helper into the value bytes pushed
// in an OP_CLAIMNAME / OP_UPDATECLAIM script: version byte +
// optional claimID + optional signature + protobuf payload.
func (h *Helper) CompileValue() ([]byte, error) {
	payload, err := h.serialize()
	if err != nil {
		return nil, err
	}

	if h.Version == WithSig {
		if len(h.ClaimID) == 0 {
			return nil, fmt.Errorf("signed claim value requires claimID")
		}
		if len(h.Signature) == 0 {
			return nil, fmt.Errorf("signed claim value requires signature")
		}
	}

	value := []byte{byte(h.Version)}
	if h.Version == WithSig {
		value = append(value, h.ClaimID...)
		value = append(value, h.Signature...)
	}
	value = append(value, payload...)

	return value, nil
}

// serialize marshals the protobuf claim.
func (h *Helper) serialize() ([]byte, error) {
	if h.Claim == nil {
		return nil, fmt.Errorf("claim not initialized")
	}
	if h.Claim.GetStream() == nil && h.Claim.GetChannel() == nil &&
		h.Claim.GetRepost() == nil && h.Claim.GetCollection() == nil {
		return nil, fmt.Errorf("claim type not set")
	}
	return h.Claim.MarshalVT()
}

// SignStream signs a stream claim with the channel's private key.
// firstInputTxID is the txid in standard display byte order (the reversed
// hex format returned by RPC and block explorers), NOT internal byte order.
// chainhash.NewHashFromStr parses display order into internal bytes.
//
// Digest = SHA-256(internalTxidBytes + claimID + protobufPayload)
// The signature is the 64-byte compact R||S from secp256k1 ECDSA.
func SignStream(h *Helper, privKey *btcec.PrivateKey, firstInputTxID string, channelClaimID []byte) error {
	if h.Claim.GetStream() == nil {
		return fmt.Errorf("helper does not contain a stream claim")
	}
	if len(channelClaimID) != 20 {
		return fmt.Errorf("channel claim ID must be 20 bytes, got %d", len(channelClaimID))
	}

	if len(firstInputTxID) != 64 {
		return fmt.Errorf("first input txid must be 64 hex characters, got %d", len(firstInputTxID))
	}
	hash, err := chainhash.NewHashFromStr(firstInputTxID)
	if err != nil {
		return fmt.Errorf("parse txid: %w", err)
	}
	txidBytes := hash[:]
	if len(txidBytes) != 32 {
		return fmt.Errorf("first input txid must be 32 bytes, got %d", len(txidBytes))
	}

	metadataBytes, err := h.serialize()
	if err != nil {
		return fmt.Errorf("serialize claim: %w", err)
	}

	digest := make([]byte, 0, len(txidBytes)+len(channelClaimID)+len(metadataBytes))
	digest = append(digest, txidBytes...)
	digest = append(digest, channelClaimID...)
	digest = append(digest, metadataBytes...)

	digestHash := sha256.Sum256(digest)

	sig, err := privKey.Sign(digestHash[:])
	if err != nil {
		return fmt.Errorf("sign claim: %w", err)
	}

	sigBytes := CompactSignature(sig)
	if len(h.ClaimID) > 0 && !bytes.Equal(h.ClaimID, channelClaimID) {
		return fmt.Errorf("channel claim ID mismatch: helper has %x, signing with %x", h.ClaimID, channelClaimID)
	}
	h.Signature = sigBytes
	h.ClaimID = channelClaimID
	h.Version = WithSig

	return nil
}

// CompactSignature converts an lbcd/btcec Signature into the 64-byte
// R||S format used by LBRY claim signatures.
//
// Applies low-S normalization to prevent signature malleability:
// if S > N/2, S is replaced with N-S. This matches btcec.Signature.Serialize().
func CompactSignature(sig *btcec.Signature) []byte {
	sigS := sig.S
	halfOrder := new(big.Int).Rsh(btcec.S256().N, 1)
	if sigS.Cmp(halfOrder) == 1 {
		sigS = new(big.Int).Sub(btcec.S256().N, sigS)
	}

	rBytes := sig.R.Bytes()
	sBytes := sigS.Bytes()
	sigBytes := make([]byte, 64)
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)
	return sigBytes
}

// ParseCompactSignature reconstructs an lbcd/btcec Signature from the 64-byte
// compact R||S format used by LBRY claim signatures.
func ParseCompactSignature(compact []byte) (*btcec.Signature, error) {
	if len(compact) != 64 {
		return nil, fmt.Errorf("compact signature must be 64 bytes, got %d", len(compact))
	}
	r := new(big.Int).SetBytes(compact[:32])
	s := new(big.Int).SetBytes(compact[32:])
	return &btcec.Signature{R: r, S: s}, nil
}

// ParseClaimValue parses a compiled claim value (as produced by CompileValue)
// back into a Helper. It handles both unsigned (v0) and signed (v1) formats.
func ParseClaimValue(data []byte) (*Helper, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("claim value too short")
	}
	version := Version(data[0])
	switch version {
	case NoSig:
		claim := &pb.Claim{}
		if err := claim.UnmarshalVT(data[1:]); err != nil {
			return nil, fmt.Errorf("unmarshal unsigned claim: %w", err)
		}
		return &Helper{Claim: claim, Version: NoSig}, nil
	case WithSig:
		if len(data) < 1+20+64 {
			return nil, fmt.Errorf("signed claim value too short: need at least 85 bytes, got %d", len(data))
		}
		claimID := append([]byte(nil), data[1:21]...)
		signature := append([]byte(nil), data[21:85]...)
		claim := &pb.Claim{}
		if err := claim.UnmarshalVT(data[85:]); err != nil {
			return nil, fmt.Errorf("unmarshal signed claim: %w", err)
		}
		return &Helper{
			Claim:     claim,
			ClaimID:   claimID,
			Signature: signature,
			Version:   WithSig,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported claim version: %d", version)
	}
}

// SupportValue serializes a support claim value.
func SupportValue(s *pb.Support) ([]byte, error) {
	return s.MarshalVT()
}
