package stream

// StreamConfig holds configuration for stream creation
type StreamConfig struct {
	ChunkHandler   func(Chunk) error
	ChunkSize      int
	ExistingSDBlob []byte
	SDHandler      func(*SDBlob, []byte) error
	Concurrency    int
	Progress       func(float64)
}

// StreamOption is a functional option for stream creation
type StreamOption func(*StreamConfig)
