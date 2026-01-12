package stream

// StreamConfig holds configuration for stream creation
type StreamConfig struct {
	ChunkHandler      func(Chunk) error
	ChunkSize         int
	ExistingSDBlob    []byte
	SDHandler         func(*SDBlob, []byte) error
	Concurrency       int
	Progress          func(float64)
	StreamName        string
	SuggestedFileName string
}

// StreamOption is a functional option for stream creation
type StreamOption func(*StreamConfig)

// WithChunkHandler sets the chunk handler function
func WithChunkHandler(handler func(Chunk) error) StreamOption {
	return func(config *StreamConfig) {
		config.ChunkHandler = handler
	}
}

// WithChunkSize sets the chunk size for streaming
func WithChunkSize(size int) StreamOption {
	return func(config *StreamConfig) {
		config.ChunkSize = size
	}
}

// WithExistingSDBlob sets an existing SD blob to use for reconstruction
func WithExistingSDBlob(sdBlob []byte) StreamOption {
	return func(config *StreamConfig) {
		config.ExistingSDBlob = sdBlob
	}
}

// WithSDHandler sets the SD blob handler function
func WithSDHandler(handler func(*SDBlob, []byte) error) StreamOption {
	return func(config *StreamConfig) {
		config.SDHandler = handler
	}
}

// WithConcurrency sets the number of concurrent workers
func WithConcurrency(workers int) StreamOption {
	return func(config *StreamConfig) {
		config.Concurrency = workers
	}
}

// WithProgress sets the progress callback function
func WithProgress(callback func(float64)) StreamOption {
	return func(config *StreamConfig) {
		config.Progress = callback
	}
}

// WithStreamName sets the stream name
func WithStreamName(name string) StreamOption {
	return func(config *StreamConfig) {
		config.StreamName = name
	}
}

// WithSuggestedFileName sets the suggested file name
func WithSuggestedFileName(name string) StreamOption {
	return func(config *StreamConfig) {
		config.SuggestedFileName = name
	}
}
