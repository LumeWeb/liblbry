package stream

// StreamResult contains the result of stream creation
type StreamResult struct {
	SDBlob        *SDBlob  `json:"sd_blob"`
	SDBlobData    []byte   `json:"sd_blob_data"`
	SDBlobHash    string   `json:"sd_blob_hash"`
	StreamHash    string   `json:"stream_hash"`
	SourceSize    int64    `json:"source_size"`
	ContentBlobs  [][]byte `json:"content_blobs,omitempty"`
	ContentHashes []string `json:"content_hashes,omitempty"`
	TotalChunks   int      `json:"total_chunks"`
	ChunkSizes    []int    `json:"chunk_sizes"`
}
