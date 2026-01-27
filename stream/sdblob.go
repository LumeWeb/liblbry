// Code adapted from github.com/lbryio/lbry.go/v2 - MIT License (c) 2016-2020 LBRY Inc.
//
// Adaptations made for liblbry:
//   - Preserved exact LBRY stream hash computation algorithm (SHA-384 based)
//   - Maintained identical test vectors and cryptographic behavior
//   - Combined functionality from sdBlob.go and json.go into a single file
//   - Adapted JSON marshaling/unmarshaling with proper field ordering for compatibility
//   - Kept core LBRY stream/blob handling logic and validation functions
//   - Verified hash computation matches upstream implementation exactly

package stream

import (
	"bytes"
	"crypto/aes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/glopal/orderedjson"
	"github.com/samber/lo"
	"go.lumeweb.com/liblbry/blob"
	lbrycrypto "go.lumeweb.com/liblbry/crypto"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
)

// Adapted from https://github.com/lbryio/lbry.go
const StreamTypeLBRYFile = "lbryfile"

// Errors
var (
	ErrInvalidSDBlob = liblbryerrors.Err("invalid SD blob")
)

// serializationProfile represents the JSON field ordering profile for SD blobs
type serializationProfile int

const (
	ProfileNewSort serializationProfile = iota // New sort: alphabetical field ordering
	ProfileOldSort                             // Old sort: legacy Python SDK field ordering
)

// profileHandler defines encoding/decoding operations for a serialization profile
type profileHandler struct {
	name string

	marshalBlobInfo   func(BlobInfo) ([]byte, error)
	unmarshalBlobInfo func([]byte) (BlobInfo, error)

	marshalSDBlob   func(SDBlob) ([]byte, error)
	unmarshalSDBlob func([]byte) (*SDBlob, error)

	predicates []profilePredicate
}

// profileRegistry maps profile IDs to their handlers
var profileRegistry = map[serializationProfile]*profileHandler{
	ProfileNewSort: newSortProfileHandler(),
	ProfileOldSort: oldSortProfileHandler(),
}

// BlobInfo contains information about a content blob
type BlobInfo struct {
	Length   int                  `json:"length"`
	BlobNum  int                  `json:"blob_num"`
	BlobHash []byte               `json:"-"`
	IV       []byte               `json:"-"`
	profile  serializationProfile // private field to track serialization profile
}

// SDBlob represents stream descriptor blob metadata
type SDBlob struct {
	StreamName        string               `json:"-"`
	BlobInfos         []BlobInfo           `json:"blobs"`
	StreamType        string               `json:"stream_type"`
	Key               []byte               `json:"-"`
	SuggestedFileName string               `json:"-"`
	StreamHash        []byte               `json:"-"`
	profile           serializationProfile // private field to track serialization profile
}

// --- JSON serialization for BlobInfo ---

// BlobInfoAlias prevents infinite recursion in MarshalJSON/UnmarshalJSON
// by using the default JSON behavior for the aliased type
type BlobInfoAlias BlobInfo

type JSONBlobInfo struct {
	BlobInfoAlias
	BlobHash string `json:"blob_hash,omitempty"`
	IV       string `json:"iv"`
}

// JSONBlobInfoNewSort represents BlobInfo with new sort (alphabetical) field ordering
// Field order: blob_hash, blob_num, iv, length
type JSONBlobInfoNewSort struct {
	BlobHash string `json:"blob_hash,omitempty"`
	BlobNum  int    `json:"blob_num"`
	IV       string `json:"iv"`
	Length   int    `json:"length"`
}

// JSONBlobInfoOldSort represents BlobInfo with old SDK field ordering
// Field order: length, blob_num, blob_hash (optional), iv
type JSONBlobInfoOldSort struct {
	Length   int    `json:"length"`
	BlobNum  int    `json:"blob_num"`
	BlobHash string `json:"blob_hash,omitempty"`
	IV       string `json:"iv"`
}

// encodeBlobInfoCommon encodes common BlobInfo fields to hex strings
func encodeBlobInfoCommon(bi BlobInfo) (string, string) {
	ivHex := hex.EncodeToString(bi.IV)
	blobHashHex := ""
	if len(bi.BlobHash) > 0 {
		blobHashHex = hex.EncodeToString(bi.BlobHash)
	}
	return ivHex, blobHashHex
}

// MarshalJSON implements custom JSON marshaling for BlobInfo
func (bi BlobInfo) MarshalJSON() ([]byte, error) {
	if handler, ok := profileRegistry[bi.profile]; ok && handler.marshalBlobInfo != nil {
		return handler.marshalBlobInfo(bi)
	}

	ivHex, blobHashHex := encodeBlobInfoCommon(bi)

	tmp := JSONBlobInfoNewSort{
		BlobHash: blobHashHex,
		BlobNum:  bi.BlobNum,
		IV:       ivHex,
		Length:   bi.Length,
	}
	return json.Marshal(tmp)
}

// UnmarshalJSON implements custom JSON unmarshaling for BlobInfo
func (bi *BlobInfo) UnmarshalJSON(b []byte) error {
	var tmp JSONBlobInfo
	err := json.Unmarshal(b, &tmp)
	if err != nil {
		return err
	}

	*bi = BlobInfo(tmp.BlobInfoAlias)

	if tmp.BlobHash != "" {
		bi.BlobHash, err = hex.DecodeString(tmp.BlobHash)
		if err != nil {
			return err
		}
	} else {
		bi.BlobHash = nil
	}

	if tmp.IV != "" {
		bi.IV, err = hex.DecodeString(tmp.IV)
		if err != nil {
			return err
		}
	} else {
		bi.IV = nil
	}

	return nil
}

// Hash returns the hash of the blob info for calculating the stream hash
func (bi BlobInfo) Hash() []byte {
	sum := sha512.New384()
	if bi.Length > 0 {
		sum.Write([]byte(hex.EncodeToString(bi.BlobHash)))
	}
	sum.Write([]byte(strconv.Itoa(bi.BlobNum)))
	sum.Write([]byte(hex.EncodeToString(bi.IV)))
	sum.Write([]byte(strconv.Itoa(bi.Length)))
	return sum.Sum(nil)
}

// --- JSON serialization for SDBlob ---

type JSONSDBlob struct {
	StreamName        string     `json:"stream_name"`
	Blobs             []BlobInfo `json:"blobs"`
	StreamType        string     `json:"stream_type"`
	Key               string     `json:"key"`
	SuggestedFileName string     `json:"suggested_file_name"`
	StreamHash        string     `json:"stream_hash"`
}

// JSONSDBlobNewSort represents SDBlob with new sort (alphabetical) field ordering
// Field order: blobs, key, stream_hash, stream_name, stream_type, suggested_file_name
type JSONSDBlobNewSort struct {
	Blobs             []JSONBlobInfoNewSort `json:"blobs"`
	Key               string                `json:"key"`
	StreamHash        string                `json:"stream_hash"`
	StreamName        string                `json:"stream_name"`
	StreamType        string                `json:"stream_type"`
	SuggestedFileName string                `json:"suggested_file_name"`
}

// JSONSDBlobOldSort represents SDBlob with old SDK field ordering
// Field order: stream_name, blobs, stream_type, key, suggested_file_name, stream_hash
type JSONSDBlobOldSort struct {
	StreamName        string                `json:"stream_name"`
	Blobs             []JSONBlobInfoOldSort `json:"blobs"`
	StreamType        string                `json:"stream_type"`
	Key               string                `json:"key"`
	SuggestedFileName string                `json:"suggested_file_name"`
	StreamHash        string                `json:"stream_hash"`
}

// encodeSDBlobCommon encodes common SDBlob fields to hex strings
func encodeSDBlobCommon(s SDBlob) (string, string, string, string) {
	streamNameHex := hex.EncodeToString([]byte(s.StreamName))
	keyHex := hex.EncodeToString(s.Key)
	suggestedFileNameHex := hex.EncodeToString([]byte(s.SuggestedFileName))
	streamHashHex := hex.EncodeToString(s.StreamHash)
	return streamNameHex, keyHex, suggestedFileNameHex, streamHashHex
}

// encodeBlobInfosAsOldSort converts BlobInfos to JSONBlobInfoOldSort slice
func encodeBlobInfosAsOldSort(blobs []BlobInfo) []JSONBlobInfoOldSort {
	return lo.Map(blobs, func(bi BlobInfo, _ int) JSONBlobInfoOldSort {
		ivHex, blobHashHex := encodeBlobInfoCommon(bi)
		return JSONBlobInfoOldSort{
			Length:   bi.Length,
			BlobNum:  bi.BlobNum,
			IV:       ivHex,
			BlobHash: blobHashHex,
		}
	})
}

// encodeBlobInfosAsNewSort converts BlobInfos to JSONBlobInfoNewSort slice
func encodeBlobInfosAsNewSort(blobs []BlobInfo) []JSONBlobInfoNewSort {
	return lo.Map(blobs, func(bi BlobInfo, _ int) JSONBlobInfoNewSort {
		ivHex, blobHashHex := encodeBlobInfoCommon(bi)
		return JSONBlobInfoNewSort{
			BlobHash: blobHashHex,
			BlobNum:  bi.BlobNum,
			IV:       ivHex,
			Length:   bi.Length,
		}
	})
}

// MarshalJSON implements custom JSON marshaling for SDBlob
func (s SDBlob) MarshalJSON() ([]byte, error) {
	if handler, ok := profileRegistry[s.profile]; ok && handler.marshalSDBlob != nil {
		return handler.marshalSDBlob(s)
	}

	streamNameHex, keyHex, suggestedFileNameHex, streamHashHex := encodeSDBlobCommon(s)

	tmp := JSONSDBlobNewSort{
		Blobs:             encodeBlobInfosAsNewSort(s.BlobInfos),
		Key:               keyHex,
		StreamHash:        streamHashHex,
		StreamName:        streamNameHex,
		StreamType:        s.StreamType,
		SuggestedFileName: suggestedFileNameHex,
	}
	return json.Marshal(tmp)
}

// decodeHex decodes a hex string to bytes, returning nil if empty
func decodeHex(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return hex.DecodeString(s)
}

// decodeHexString decodes a hex string to string, returning empty string if input is empty
func decodeHexString(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeSDBlobFields decodes common SDBlob fields from hex
func decodeSDBlobFields(streamNameHex, keyHex, suggestedFileNameHex, streamHashHex string) (string, []byte, string, []byte, error) {
	streamName, err := decodeHexString(streamNameHex)
	if err != nil {
		return "", nil, "", nil, err
	}

	key, err := decodeHex(keyHex)
	if err != nil {
		return "", nil, "", nil, err
	}

	suggestedFileName, err := decodeHexString(suggestedFileNameHex)
	if err != nil {
		return "", nil, "", nil, err
	}

	streamHash, err := decodeHex(streamHashHex)
	if err != nil {
		return "", nil, "", nil, err
	}

	return streamName, key, suggestedFileName, streamHash, nil
}

// decodeBlobInfoFromNewSort decodes JSONBlobInfoNewSort to BlobInfo
func decodeBlobInfoFromNewSort(tmp JSONBlobInfoNewSort, profile serializationProfile) (BlobInfo, error) {
	bi := BlobInfo{
		Length:  tmp.Length,
		BlobNum: tmp.BlobNum,
		profile: profile,
	}

	if tmp.BlobHash != "" {
		blobHash, err := hex.DecodeString(tmp.BlobHash)
		if err != nil {
			return BlobInfo{}, err
		}
		bi.BlobHash = blobHash
	}

	if tmp.IV != "" {
		iv, err := hex.DecodeString(tmp.IV)
		if err != nil {
			return BlobInfo{}, err
		}
		bi.IV = iv
	}

	return bi, nil
}

// decodeBlobInfoFromOldSort decodes JSONBlobInfoOldSort to BlobInfo
func decodeBlobInfoFromOldSort(tmp JSONBlobInfoOldSort, profile serializationProfile) (BlobInfo, error) {
	bi := BlobInfo{
		Length:  tmp.Length,
		BlobNum: tmp.BlobNum,
		profile: profile,
	}

	if tmp.BlobHash != "" {
		blobHash, err := hex.DecodeString(tmp.BlobHash)
		if err != nil {
			return BlobInfo{}, err
		}
		bi.BlobHash = blobHash
	}

	if tmp.IV != "" {
		iv, err := hex.DecodeString(tmp.IV)
		if err != nil {
			return BlobInfo{}, err
		}
		bi.IV = iv
	}

	return bi, nil
}

// UnmarshalJSON implements custom JSON unmarshaling for SDBlob
func (s *SDBlob) UnmarshalJSON(b []byte) error {
	s.profile = detectSerializationProfile(b)

	if handler, ok := profileRegistry[s.profile]; ok && handler.unmarshalSDBlob != nil {
		result, err := handler.unmarshalSDBlob(b)
		if err != nil {
			return err
		}
		*s = *result
		return nil
	}

	return fmt.Errorf("no unmarshal handler for profile: %d", s.profile)
}

// ToJson returns the SD blob as JSON with indentation
func (s SDBlob) ToJson() (string, error) {
	j, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	return string(j), nil
}

// ToBlob converts the SDBlob to a normal data Blob
func (s SDBlob) ToBlob() ([]byte, error) {
	jsonSD, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}

	// COMPATIBILITY HACK to make json output match python's json. this can be
	// removed when we implement canonical JSON encoding
	jsonSD = []byte(strings.Replace(string(jsonSD), ",", ", ", -1))
	jsonSD = []byte(strings.Replace(string(jsonSD), ":", ": ", -1))

	return jsonSD, nil
}

// IsValid checks if the SD blob is valid by comparing its stored stream hash with computed stream hash
func (s SDBlob) IsValid() bool {
	computedHash := s.computeStreamHash()
	return bytes.Equal(computedHash, s.StreamHash)
}

// profilePredicate checks if the JSON matches a specific serialization profile
type profilePredicate func(orderedjson.Map) (serializationProfile, bool)

// detectSerializationProfile analyzes JSON bytes to determine the serialization profile
// by applying predicates from registered profile handlers in order and returning the first match
func detectSerializationProfile(b []byte) serializationProfile {
	var orderedMap orderedjson.Map
	if err := json.Unmarshal(b, &orderedMap); err != nil {
		return ProfileNewSort
	}

	for _, handler := range profileRegistry {
		for _, predicate := range handler.predicates {
			if detectedProfile, detected := predicate(orderedMap); detected {
				return detectedProfile
			}
		}
	}

	return ProfileNewSort
}

// FromBlob unmarshals a data Blob that should contain SDBlob data
func (s *SDBlob) FromBlob(b []byte) error {
	s.profile = detectSerializationProfile(b)
	return json.Unmarshal(b, s)
}

// SetProfile sets the serialization profile for the SDBlob and all its BlobInfos
func (s *SDBlob) SetProfile(profile serializationProfile) {
	s.profile = profile
	for i := range s.BlobInfos {
		s.BlobInfos[i].profile = profile
	}
}

// GetProfile returns the serialization profile for the SDBlob
func (s *SDBlob) GetProfile() serializationProfile {
	return s.profile
}

// Hash returns a hash of the SD blob data
func (s SDBlob) Hash() []byte {
	blobData, _ := s.ToBlob()
	hashBytes := sha512.Sum384(blobData)
	return hashBytes[:]
}

// HashHex returns the SD blob hash as a hex string
func (s SDBlob) HashHex() string {
	return hex.EncodeToString(s.Hash())
}

// streamHash computes the stream hash using the LBRY algorithm
func streamHash(hexStreamName, hexKey, hexSuggestedFileName string, blobInfos []BlobInfo) []byte {
	blobSum := sha512.New384()
	for _, b := range blobInfos {
		blobSum.Write(b.Hash())
	}

	sum := sha512.New384()
	sum.Write([]byte(hexStreamName))
	sum.Write([]byte(hexKey))
	sum.Write([]byte(hexSuggestedFileName))
	sum.Write(blobSum.Sum(nil))
	return sum.Sum(nil)
}

// NullIV returns a null initialization vector
func NullIV() []byte {
	return make([]byte, aes.BlockSize)
}

// addBlob adds a blob to the SDBlob
func (s *SDBlob) addBlob(b blob.Blob, iv []byte) error {
	if len(iv) == 0 {
		return fmt.Errorf("empty IV")
	}
	blobHash := b.Hash()
	s.BlobInfos = append(s.BlobInfos, BlobInfo{
		BlobNum:  len(s.BlobInfos),
		Length:   len(b),
		BlobHash: blobHash,
		IV:       iv,
	})
	return nil
}

// UpdateStreamHash updates the stream hash of the SDBlob
func (s *SDBlob) UpdateStreamHash() {
	s.StreamHash = s.computeStreamHash()
}

// computeStreamHash computes the stream hash of the SDBlob
func (s *SDBlob) computeStreamHash() []byte {
	return streamHash(
		hex.EncodeToString([]byte(s.StreamName)),
		hex.EncodeToString(s.Key),
		hex.EncodeToString([]byte(s.SuggestedFileName)),
		s.BlobInfos,
	)
}

// newSortProfileHandler creates a handler for new sort (alphabetical) serialization
func newSortProfileHandler() *profileHandler {
	return &profileHandler{
		name: "new_sort",
		marshalBlobInfo: func(bi BlobInfo) ([]byte, error) {
			ivHex, blobHashHex := encodeBlobInfoCommon(bi)
			tmp := JSONBlobInfoNewSort{
				BlobHash: blobHashHex,
				BlobNum:  bi.BlobNum,
				IV:       ivHex,
				Length:   bi.Length,
			}
			return json.Marshal(tmp)
		},
		unmarshalBlobInfo: func(b []byte) (BlobInfo, error) {
			var tmp JSONBlobInfoNewSort
			if err := json.Unmarshal(b, &tmp); err != nil {
				return BlobInfo{}, err
			}

			bi := BlobInfo{
				Length:  tmp.Length,
				BlobNum: tmp.BlobNum,
				profile: ProfileNewSort,
			}

			if tmp.BlobHash != "" {
				blobHash, err := hex.DecodeString(tmp.BlobHash)
				if err != nil {
					return BlobInfo{}, err
				}
				bi.BlobHash = blobHash
			}

			if tmp.IV != "" {
				iv, err := hex.DecodeString(tmp.IV)
				if err != nil {
					return BlobInfo{}, err
				}
				bi.IV = iv
			}

			return bi, nil
		},
		marshalSDBlob: func(s SDBlob) ([]byte, error) {
			streamNameHex, keyHex, suggestedFileNameHex, streamHashHex := encodeSDBlobCommon(s)
			tmp := JSONSDBlobNewSort{
				Blobs:             encodeBlobInfosAsNewSort(s.BlobInfos),
				Key:               keyHex,
				StreamHash:        streamHashHex,
				StreamName:        streamNameHex,
				StreamType:        s.StreamType,
				SuggestedFileName: suggestedFileNameHex,
			}
			return json.Marshal(tmp)
		},
		unmarshalSDBlob: func(b []byte) (*SDBlob, error) {
			var tmp JSONSDBlobNewSort
			if err := json.Unmarshal(b, &tmp); err != nil {
				return nil, err
			}

			s := &SDBlob{profile: ProfileNewSort}

			var err error
			s.StreamName, s.Key, s.SuggestedFileName, s.StreamHash, err = decodeSDBlobFields(tmp.StreamName, tmp.Key, tmp.SuggestedFileName, tmp.StreamHash)
			if err != nil {
				return nil, err
			}

			s.StreamType = tmp.StreamType

			blobInfos := make([]BlobInfo, len(tmp.Blobs))
			for i, bi := range tmp.Blobs {
				result, err := decodeBlobInfoFromNewSort(bi, ProfileNewSort)
				if err != nil {
					return nil, err
				}
				blobInfos[i] = result
			}
			s.BlobInfos = blobInfos

			return s, nil
		},
		predicates: []profilePredicate{
			func(m orderedjson.Map) (serializationProfile, bool) {
				if len(m) == 0 {
					return ProfileNewSort, false
				}
				firstKey := string(m[0].Key)
				if firstKey == `"blobs"` {
					return ProfileNewSort, true
				}
				return ProfileNewSort, false
			},
		},
	}
}

// oldSortProfileHandler creates a handler for old sort (legacy Python SDK) serialization
func oldSortProfileHandler() *profileHandler {
	return &profileHandler{
		name: "old_sort",
		marshalBlobInfo: func(bi BlobInfo) ([]byte, error) {
			ivHex, blobHashHex := encodeBlobInfoCommon(bi)
			tmp := JSONBlobInfoOldSort{
				Length:   bi.Length,
				BlobNum:  bi.BlobNum,
				IV:       ivHex,
				BlobHash: blobHashHex,
			}
			return json.Marshal(tmp)
		},
		unmarshalBlobInfo: func(b []byte) (BlobInfo, error) {
			var tmp JSONBlobInfoOldSort
			if err := json.Unmarshal(b, &tmp); err != nil {
				return BlobInfo{}, err
			}

			bi := BlobInfo{
				Length:  tmp.Length,
				BlobNum: tmp.BlobNum,
				profile: ProfileOldSort,
			}

			if tmp.BlobHash != "" {
				blobHash, err := hex.DecodeString(tmp.BlobHash)
				if err != nil {
					return BlobInfo{}, err
				}
				bi.BlobHash = blobHash
			}

			if tmp.IV != "" {
				iv, err := hex.DecodeString(tmp.IV)
				if err != nil {
					return BlobInfo{}, err
				}
				bi.IV = iv
			}

			return bi, nil
		},
		marshalSDBlob: func(s SDBlob) ([]byte, error) {
			streamNameHex, keyHex, suggestedFileNameHex, streamHashHex := encodeSDBlobCommon(s)
			tmp := JSONSDBlobOldSort{
				StreamName:        streamNameHex,
				StreamType:        s.StreamType,
				Key:               keyHex,
				SuggestedFileName: suggestedFileNameHex,
				StreamHash:        streamHashHex,
				Blobs:             encodeBlobInfosAsOldSort(s.BlobInfos),
			}
			return json.Marshal(tmp)
		},
		unmarshalSDBlob: func(b []byte) (*SDBlob, error) {
			var tmp JSONSDBlobOldSort
			if err := json.Unmarshal(b, &tmp); err != nil {
				return nil, err
			}

			s := &SDBlob{profile: ProfileOldSort}

			var err error
			s.StreamName, s.Key, s.SuggestedFileName, s.StreamHash, err = decodeSDBlobFields(tmp.StreamName, tmp.Key, tmp.SuggestedFileName, tmp.StreamHash)
			if err != nil {
				return nil, err
			}

			s.StreamType = tmp.StreamType

			blobInfos := make([]BlobInfo, len(tmp.Blobs))
			for i, bi := range tmp.Blobs {
				result, err := decodeBlobInfoFromOldSort(bi, ProfileOldSort)
				if err != nil {
					return nil, err
				}
				blobInfos[i] = result
			}
			s.BlobInfos = blobInfos

			return s, nil
		},
		predicates: []profilePredicate{
			func(m orderedjson.Map) (serializationProfile, bool) {
				if len(m) == 0 {
					return ProfileNewSort, false
				}
				firstKey := string(m[0].Key)
				if firstKey == `"stream_name"` {
					return ProfileOldSort, true
				}
				return ProfileNewSort, false
			},
		},
	}
}

// ValidateSDBlob validates an SDBlob struct's structure and content
func ValidateSDBlob(sd *SDBlob) error {
	if sd == nil {
		return ErrInvalidSDBlob
	}
	return validateSDBlob(sd)
}

// ValidateSDBlobBytes validates SD blob structure and content from raw bytes
func ValidateSDBlobBytes(sdBlobData []byte) error {
	if len(sdBlobData) == 0 {
		return ErrInvalidSDBlob
	}

	if !json.Valid(sdBlobData) {
		return ErrInvalidSDBlob
	}

	var sdBlob SDBlob
	if err := sdBlob.FromBlob(sdBlobData); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSDBlob, err)
	}

	return validateSDBlob(&sdBlob)
}

// validateSDBlob validates an SDBlob struct's structure and content
func validateSDBlob(sd *SDBlob) error {
	// Validate required fields
	if sd.StreamType == "" {
		return fmt.Errorf("%w: missing stream_type", ErrInvalidSDBlob)
	}

	if len(sd.BlobInfos) == 0 {
		return fmt.Errorf("%w: no blobs found", ErrInvalidSDBlob)
	}

	// Validate key size
	if len(sd.Key) != lbrycrypto.AES256KeySize && len(sd.Key) != lbrycrypto.AES128KeySize {
		return fmt.Errorf("%w: invalid key size: expected %d or %d bytes, got %d bytes", ErrInvalidSDBlob, lbrycrypto.AES256KeySize, lbrycrypto.AES128KeySize, len(sd.Key))
	}

	// Validate each blob info
	terminatingBlobFound := false
	terminatingBlobIndex := -1
	totalBlobs := len(sd.BlobInfos)
	for i, blobInfo := range sd.BlobInfos {
		// Validate blob number
		if blobInfo.BlobNum != i {
			return fmt.Errorf("%w: blob %d has invalid blob_num: expected %d, got %d", ErrInvalidSDBlob, i, i, blobInfo.BlobNum)
		}

		// Zero-length blobs (terminating blobs) are allowed to have missing hashes
		if blobInfo.Length > 0 {
			if len(blobInfo.BlobHash) == 0 {
				return fmt.Errorf("%w: blob %d missing hash", ErrInvalidSDBlob, i)
			}
			if !ValidateHash(hex.EncodeToString(blobInfo.BlobHash)) {
				return fmt.Errorf("%w: blob %d has invalid hash", ErrInvalidSDBlob, i)
			}
		}
		if blobInfo.Length < 0 {
			return fmt.Errorf("%w: blob %d has invalid length", ErrInvalidSDBlob, i)
		}

		// Validate IV
		if len(blobInfo.IV) == 0 {
			return fmt.Errorf("%w: blob %d missing IV", ErrInvalidSDBlob, i)
		}

		// Track terminating blobs (zero-length blobs)
		if blobInfo.Length == 0 {
			if terminatingBlobFound {
				return fmt.Errorf("%w: terminating blob can only appear once", ErrInvalidSDBlob)
			}
			terminatingBlobFound = true
			terminatingBlobIndex = i
		}
	}

	// After validating all blobs, check if terminating blob is at the end (if present)
	if terminatingBlobFound && terminatingBlobIndex != totalBlobs-1 {
		return fmt.Errorf("%w: terminating blob must be at the end", ErrInvalidSDBlob)
	}

	// Validate stream hash
	if len(sd.StreamHash) == 0 {
		return fmt.Errorf("%w: missing stream hash", ErrInvalidSDBlob)
	}

	computedHash := sd.computeStreamHash()
	if !bytes.Equal(sd.StreamHash, computedHash) {
		return fmt.Errorf("%w: stream hash mismatch: computed hash does not match stored hash", ErrInvalidSDBlob)
	}

	return nil
}
