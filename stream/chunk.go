package stream

// Chunk represents a chunk of data for streaming operations
type Chunk struct {
	Number   int    `json:"number"`
	Hash     string `json:"hash"`
	Data     []byte `json:"data"`
	Size     int    `json:"size"`
	StreamID string `json:"stream_id"`
}
