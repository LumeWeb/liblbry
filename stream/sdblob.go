package stream

// Adapted from https://github.com/lbryio/lbry.go

// TODO: StreamType field and streamTypeLBRYFile constant were removed from the original lbry.go implementation
// because they serve no functional purpose in the core blob/stream processing logic.
//
// However, these may need to be added back if:
// - Legacy LBRY clients or servers specifically validate the "stream_type" field
// - Protocol compatibility issues arise with existing infrastructure
// - JSON serialization/deserialization expects the field to be present
//
// Original structure from lbry.go included:
//   const streamTypeLBRYFile = "lbryfile"
//   StreamType string `json:"stream_type"`
//
// To restore compatibility, add:
//   const streamTypeLBRYFile = "lbryfile"
//   StreamType string `json:"stream_type"` to SDBlob struct
//   StreamType: streamTypeLBRYFile in encoder initialization

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
	Key               []byte     `json:"-"`
	SuggestedFileName string     `json:"-"`
	StreamHash        []byte     `json:"-"`
}
