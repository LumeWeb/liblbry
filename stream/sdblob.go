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
	"strconv"
	"strings"
)

// Adapted from https://github.com/lbryio/lbry.go
const streamTypeLBRYFile = "lbryfile"

// BlobInfo contains information about a content blob
type BlobInfo struct {
	Length   int    `json:"length"`
	BlobNum  int    `json:"blob_num"`
	BlobHash []byte `json:"-"`
	IV       []byte `json:"-"`
}

// SDBlob represents stream descriptor blob metadata
type SDBlob struct {
	StreamName        string     `json:"-"`
	BlobInfos         []BlobInfo `json:"blobs"`
	StreamType        string     `json:"stream_type"`
	Key               []byte     `json:"-"`
	SuggestedFileName string     `json:"-"`
	StreamHash        []byte     `json:"-"`
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

// MarshalJSON implements custom JSON marshaling for BlobInfo
func (bi BlobInfo) MarshalJSON() ([]byte, error) {
	var tmp JSONBlobInfo

	tmp.IV = hex.EncodeToString(bi.IV)
	if len(bi.BlobHash) > 0 {
		tmp.BlobHash = hex.EncodeToString(bi.BlobHash)
	}

	tmp.BlobInfoAlias = BlobInfoAlias(bi)

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

// SDBlobAlias prevents infinite recursion in MarshalJSON/UnmarshalJSON by
// creating a type with the same structure but without the custom JSON methods
type SDBlobAlias SDBlob

type JSONSDBlob struct {
	StreamName string `json:"stream_name"`
	SDBlobAlias
	StreamType        string `json:"stream_type"`
	Key               string `json:"key"`
	SuggestedFileName string `json:"suggested_file_name"`
	StreamHash        string `json:"stream_hash"`
}

// MarshalJSON implements custom JSON marshaling for SDBlob
func (s SDBlob) MarshalJSON() ([]byte, error) {
	var tmp JSONSDBlob

	tmp.StreamName = hex.EncodeToString([]byte(s.StreamName))
	tmp.StreamType = streamTypeLBRYFile
	tmp.StreamHash = hex.EncodeToString(s.StreamHash)
	tmp.SuggestedFileName = hex.EncodeToString([]byte(s.SuggestedFileName))
	tmp.Key = hex.EncodeToString(s.Key)

	tmp.SDBlobAlias = SDBlobAlias(s)

	return json.Marshal(tmp)
}

// UnmarshalJSON implements custom JSON unmarshaling for SDBlob
func (s *SDBlob) UnmarshalJSON(b []byte) error {
	var tmp JSONSDBlob
	err := json.Unmarshal(b, &tmp)
	if err != nil {
		return err
	}

	*s = SDBlob(tmp.SDBlobAlias)

	if tmp.StreamName != "" {
		str, err := hex.DecodeString(tmp.StreamName)
		if err != nil {
			return err
		}
		s.StreamName = string(str)
	} else {
		s.StreamName = ""
	}

	if tmp.SuggestedFileName != "" {
		str, err := hex.DecodeString(tmp.SuggestedFileName)
		if err != nil {
			return err
		}
		s.SuggestedFileName = string(str)
	} else {
		s.SuggestedFileName = ""
	}

	if tmp.StreamHash != "" {
		s.StreamHash, err = hex.DecodeString(tmp.StreamHash)
		if err != nil {
			return err
		}
	} else {
		s.StreamHash = nil
	}

	if tmp.Key != "" {
		s.Key, err = hex.DecodeString(tmp.Key)
		if err != nil {
			return err
		}
	} else {
		s.Key = nil
	}

	return nil
}

// ToJson returns the SD blob as JSON with indentation
func (s SDBlob) ToJson() string {
	j, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(j)
}

// ToBlob converts the SDBlob to a normal data Blob
func (s SDBlob) ToBlob() []byte {
	jsonSD, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}

	// COMPATIBILITY HACK to make json output match python's json. this can be
	// removed when we implement canonical JSON encoding
	jsonSD = []byte(strings.Replace(string(jsonSD), ",", ", ", -1))
	jsonSD = []byte(strings.Replace(string(jsonSD), ":", ": ", -1))

	return jsonSD
}

// IsValid checks if the SD blob is valid by comparing its computed hash with the stored hash
func (s SDBlob) IsValid() bool {
	computedHash := s.Hash()
	return bytes.Equal(computedHash, s.StreamHash)
}

// FromBlob unmarshals a data Blob that should contain SDBlob data
func (s *SDBlob) FromBlob(b []byte) error {
	return json.Unmarshal(b, s)
}

// Hash returns a hash of the SD blob data
func (s SDBlob) Hash() []byte {
	return s.computeStreamHash()
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

// computeBlobHash computes the hash of a blob
func computeBlobHash(b Blob) []byte {
	hasher := NewHasher()
	hashStr := hasher.Hash([]byte(b))
	hash, _ := hex.DecodeString(hashStr)
	return hash
}

// NullIV returns a null initialization vector
func NullIV() []byte {
	return make([]byte, aes.BlockSize)
}

// addBlob adds a blob to the SDBlob
func (s *SDBlob) addBlob(b Blob, iv []byte) {
	if len(iv) == 0 {
		panic("empty IV")
	}
	s.BlobInfos = append(s.BlobInfos, BlobInfo{
		BlobNum:  len(s.BlobInfos),
		Length:   len(b),
		BlobHash: computeBlobHash(b),
		IV:       iv,
	})
}

// updateStreamHash updates the stream hash of the SDBlob
func (s *SDBlob) updateStreamHash() {
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
