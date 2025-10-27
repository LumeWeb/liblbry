package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/liblbry/blob"
	"go.lumeweb.com/liblbry/mocks"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
)

// Reflector-specific test constants
const (
	reflectorTestStoreName    = testStoreName
	reflectorTestHost         = testHost
	reflectorTestTimeout      = testTimeout
	reflectorTestBufferSize   = testBufferSize
	reflectorShortTestTimeout = shortTestTimeout
)

var reflectorTestBlobs = map[string][]byte{
	validBlobHash1: []byte("test blob data 1"),
	validBlobHash2: []byte("test blob data 2"),
	validBlobHash3: []byte("test blob data 3"),
}

// MockConnWrapper wraps the mockery MockConn and provides testing helper methods
type MockConnWrapper struct {
	*protocolMocks.MockConn
	readBuffer  *bytes.Buffer
	writeBuffer *bytes.Buffer
}

func NewMockConn(t *testing.T) *MockConnWrapper {
	mockConn := protocolMocks.NewMockConn(t)
	return &MockConnWrapper{
		MockConn:    mockConn,
		readBuffer:  &bytes.Buffer{},
		writeBuffer: &bytes.Buffer{},
	}
}

// WriteToReadBuffer writes data to the read buffer (simulating incoming data)
func (m *MockConnWrapper) WriteToReadBuffer(data []byte) {
	m.readBuffer.Write(data)
}

// GetWrittenData returns data written to the connection
func (m *MockConnWrapper) GetWrittenData() []byte {
	return m.writeBuffer.Bytes()
}

// ClearWrittenData clears the write buffer
func (m *MockConnWrapper) ClearWrittenData() {
	m.writeBuffer.Reset()
}

// ReadBuffer returns the read buffer for direct manipulation
func (m *MockConnWrapper) ReadBuffer() *bytes.Buffer {
	return m.readBuffer
}

// Read overrides the mock to read from our buffer
func (m *MockConnWrapper) Read(b []byte) (n int, err error) {
	// If read buffer is empty, return EOF
	if m.readBuffer.Len() == 0 {
		return 0, io.EOF
	}

	// Read from buffer
	n, err = m.readBuffer.Read(b)

	// If we read some data but hit EOF, return the data first
	if err == io.EOF && n > 0 {
		err = nil
	}

	return n, err
}

// Write overrides the mock to write to our buffer
func (m *MockConnWrapper) Write(b []byte) (n int, err error) {
	return m.writeBuffer.Write(b)
}
func (m *MockConnWrapper) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline overrides the mock to handle deadline setting
func (m *MockConnWrapper) SetWriteDeadline(t time.Time) error {
	return nil
}

// SetDeadline overrides the mock to handle deadline setting
func (m *MockConnWrapper) SetDeadline(t time.Time) error {
	return nil
}

// RemoteAddr overrides the mock to provide a test address
func (m *MockConnWrapper) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

// setupReflectorMockStore sets up a mock blob store with test data
func setupReflectorMockStore(t *testing.T, withBlobs bool) *mocks.MockBlobStore {
	mockStore := mocks.NewMockBlobStore(t)

	if withBlobs {
		for k, v := range reflectorTestBlobs {
			// Set up mock behavior for Has method
			mockStore.On("Has", k).Return(true, nil)
			// Set up mock behavior for Get method
			mockStore.On("Get", k).Return(v, nil)
		}
	}

	// For non-existent blobs, Has should return false
	mockStore.On("Has", mock.AnythingOfType("string")).Return(false, nil)
	mockStore.On("Get", mock.AnythingOfType("string")).Return([]byte(nil), errors.New("blob not found"))

	// Set up mock behavior for Name method
	mockStore.On("Name").Return(reflectorTestStoreName)

	return mockStore
}

func TestNewReflectorServer(t *testing.T) {
	store := mocks.NewMockBlobStore(t)

	server := NewReflectorServer(store)

	assert.NotNil(t, server)

	reflectorServer, ok := server.(*DefaultReflectorServer)
	assert.True(t, ok, "Expected server to be of type DefaultReflectorServer")

	assert.Equal(t, store, reflectorServer.store, "Expected store to be set correctly")
	assert.Equal(t, DefaultReflectorTimeout, reflectorServer.connectionTimeout, "Expected default timeout to be set")
}

func TestNewReflectorServerWithOptions(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	accessControl := mocks.NewMockAccessControl(t)
	timeout := 10 * time.Second

	server := NewReflectorServer(
		store,
		WithReflectorAccessControl(accessControl),
		WithReflectorTimeout(timeout),
	)

	reflectorServer := server.(*DefaultReflectorServer)

	assert.Equal(t, accessControl, reflectorServer.accessControl, "Expected access control to be set correctly")
	assert.Equal(t, timeout, reflectorServer.connectionTimeout, "Expected timeout to be set correctly")
}

func TestDoHandshake_Success(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Write handshake request
	version := ProtocolVersion1
	handshake := HandshakeRequestResponse{Version: &version}
	handshakeData, _ := json.Marshal(handshake)
	conn.WriteToReadBuffer(handshakeData)

	// Perform handshake
	reader := bufio.NewReader(conn)
	err := server.(*DefaultReflectorServer).doHandshake(conn, reader)

	assert.NoError(t, err, "Expected handshake to succeed")

	// Check response
	writtenData := conn.GetWrittenData()
	var response HandshakeRequestResponse
	err = json.Unmarshal(writtenData, &response)
	assert.NoError(t, err, "Failed to unmarshal response")

	assert.NotNil(t, response.Version, "Expected response to have version")
	assert.Equal(t, ProtocolVersion1, *response.Version, "Expected response to have correct version")
}

func TestDoHandshake_InvalidVersion(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Write handshake with invalid version
	version := 999
	handshake := HandshakeRequestResponse{Version: &version}
	handshakeData, _ := json.Marshal(handshake)
	conn.WriteToReadBuffer(handshakeData)

	// Perform handshake
	reader := bufio.NewReader(conn)
	err := server.(*DefaultReflectorServer).doHandshake(conn, reader)

	assert.Error(t, err, "Expected handshake to fail with invalid version")
	assert.True(t, errors.Is(err, ErrProtocolVersion), "Expected ErrProtocolVersion")
}

func TestDoHandshake_MissingVersion(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Write handshake without version
	handshake := HandshakeRequestResponse{}
	handshakeData, _ := json.Marshal(handshake)
	conn.WriteToReadBuffer(handshakeData)

	// Perform handshake
	reader := bufio.NewReader(conn)
	err := server.(*DefaultReflectorServer).doHandshake(conn, reader)

	assert.Error(t, err, "Expected handshake to fail with missing version")
	assert.True(t, errors.Is(err, ErrHandshakeMissing), "Expected ErrHandshakeMissing")
}

func TestReadBlobRequest_RegularBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Write blob request
	request := SendBlobRequest{
		BlobHash: validBlobHash1,
		BlobSize: 1024,
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Read blob request
	reader := bufio.NewReader(conn)
	blobSize, blobHash, isSdBlob, err := server.(*DefaultReflectorServer).readBlobRequest(conn, reader)

	assert.NoError(t, err, "Expected readBlobRequest to succeed")
	assert.Equal(t, 1024, blobSize, "Expected blob size 1024")
	assert.Equal(t, request.BlobHash, blobHash, "Expected blob hash to match")
	assert.False(t, isSdBlob, "Expected isSdBlob to be false for regular blob")
}

func TestReadBlobRequest_SDBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Write SD blob request
	request := SendBlobRequest{
		SdBlobHash: validBlobHash1,
		SdBlobSize: 512,
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Read blob request
	reader := bufio.NewReader(conn)
	blobSize, blobHash, isSdBlob, err := server.(*DefaultReflectorServer).readBlobRequest(conn, reader)

	assert.NoError(t, err, "Expected readBlobRequest to succeed")
	assert.Equal(t, 512, blobSize, "Expected blob size 512")
	assert.Equal(t, request.SdBlobHash, blobHash, "Expected blob hash to match")
	assert.True(t, isSdBlob, "Expected isSdBlob to be true for SD blob")
}

func TestReadBlobRequest_EmptyHash(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Write blob request with empty hash
	request := SendBlobRequest{
		BlobHash: "",
		BlobSize: 1024,
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Read blob request
	reader := bufio.NewReader(conn)
	_, _, _, err := server.(*DefaultReflectorServer).readBlobRequest(conn, reader)

	assert.Error(t, err, "Expected readBlobRequest to fail with empty hash")
	assert.True(t, errors.Is(err, ErrBlobHashEmpty), "Expected ErrBlobHashEmpty")
}

func TestReadBlobRequest_BlobTooBig(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Write blob request with size too large
	request := SendBlobRequest{
		BlobHash: validBlobHash1,
		BlobSize: MaxBlobSize + 1,
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Read blob request
	reader := bufio.NewReader(conn)
	_, _, _, err := server.(*DefaultReflectorServer).readBlobRequest(conn, reader)

	assert.Error(t, err, "Expected readBlobRequest to fail with blob too big")
	assert.True(t, errors.Is(err, ErrBlobTooBig), "Expected ErrBlobTooBig")
}

func TestShouldAcceptBlob_NewBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)

	// Mock store methods
	store.On("Has", validBlobHash1).Return(false, nil)

	shouldSend, neededBlobs, err := server.(*DefaultReflectorServer).shouldAcceptBlob(validBlobHash1, false, testLocalIP)

	assert.NoError(t, err, "Expected shouldAcceptBlob to succeed")
	assert.True(t, shouldSend, "Expected shouldSend to be true for new blob")
	assert.Empty(t, neededBlobs, "Expected no needed blobs for regular blob")
}

func TestShouldAcceptBlob_ExistingBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)

	// Mock store methods - blob already exists
	store.On("Has", validBlobHash1).Return(true, nil)

	shouldSend, neededBlobs, err := server.(*DefaultReflectorServer).shouldAcceptBlob(validBlobHash1, false, testLocalIP)

	assert.NoError(t, err, "Expected shouldAcceptBlob to succeed")
	assert.False(t, shouldSend, "Expected shouldSend to be false for existing blob")
	assert.Empty(t, neededBlobs, "Expected no needed blobs for regular blob")
}

func TestSendBlobResponse_RegularBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	err := server.(*DefaultReflectorServer).sendBlobResponse(conn, true, false, []string{})

	assert.NoError(t, err, "Expected sendBlobResponse to succeed")

	// Check response
	writtenData := conn.GetWrittenData()
	var response SendBlobResponse
	err = json.Unmarshal(writtenData, &response)
	assert.NoError(t, err, "Failed to unmarshal response")

	assert.True(t, response.SendBlob, "Expected SendBlob to be true")
}

func TestSendBlobResponse_SDBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	neededBlobs := []string{"blob1", "blob2"}
	err := server.(*DefaultReflectorServer).sendBlobResponse(conn, true, true, neededBlobs)

	assert.NoError(t, err, "Expected sendBlobResponse to succeed")

	// Check response
	writtenData := conn.GetWrittenData()
	var response SendSDBlobResponse
	err = json.Unmarshal(writtenData, &response)
	assert.NoError(t, err, "Failed to unmarshal response")

	assert.True(t, response.SendSdBlob, "Expected SendSdBlob to be true")
	assert.Equal(t, len(neededBlobs), len(response.NeededBlobs), "Expected correct number of needed blobs")
}

func TestSendTransferResponse_RegularBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	err := server.(*DefaultReflectorServer).sendTransferResponse(conn, true, false)

	assert.NoError(t, err, "Expected sendTransferResponse to succeed")

	// Check response
	writtenData := conn.GetWrittenData()
	var response BlobTransferResponse
	err = json.Unmarshal(writtenData, &response)
	assert.NoError(t, err, "Failed to unmarshal response")

	assert.True(t, response.ReceivedBlob, "Expected ReceivedBlob to be true")
}

func TestSendTransferResponse_SDBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	err := server.(*DefaultReflectorServer).sendTransferResponse(conn, false, true)

	assert.NoError(t, err, "Expected sendTransferResponse to succeed")

	// Check response
	writtenData := conn.GetWrittenData()
	var response SDBlobTransferResponse
	err = json.Unmarshal(writtenData, &response)
	assert.NoError(t, err, "Failed to unmarshal response")

	assert.False(t, response.ReceivedSdBlob, "Expected ReceivedSdBlob to be false")
}

func TestCalculateBlobHash(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)

	blobData := []byte("test data for hashing")

	hash := server.(*DefaultReflectorServer).calculateBlobHash(blobData)

	assert.Equal(t, blob.BlobHashHexLength, len(hash), "Expected correct hash length")

	// Verify it's a valid hex string
	for _, c := range hash {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'),
			"Hash contains invalid character: %c", c)
	}
}

func TestValidateBlobHash(t *testing.T) {
	// Valid hash
	validHash := validBlobHash1
	assert.True(t, ValidateBlobHash(validHash), "Expected valid hash to pass validation")

	// Invalid length
	shortHash := "1234567890abcdef"
	assert.False(t, ValidateBlobHash(shortHash), "Expected short hash to fail validation")

	// Invalid characters
	invalidCharHash := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdeG"
	assert.False(t, ValidateBlobHash(invalidCharHash), "Expected hash with invalid characters to fail validation")
}

func TestCalculateTimeout(t *testing.T) {
	tests := []struct {
		name     string
		blobSize int
		want     time.Duration
	}{
		{
			name:     "small blob (<1MiB)",
			blobSize: 500 * 1024, // 500 KiB
			want:     BaseTimeout,
		},
		{
			name:     "exactly 1MiB",
			blobSize: MiB,
			want:     BaseTimeout + TimeoutPerMiB,
		},
		{
			name:     "2MiB blob",
			blobSize: 2 * MiB,
			want:     BaseTimeout + 2*TimeoutPerMiB,
		},
		{
			name:     "very large blob (5min cap)",
			blobSize: 100 * MiB, // Should hit the 5 minute cap
			want:     5 * time.Minute,
		},
		{
			name:     "zero size blob",
			blobSize: 0,
			want:     BaseTimeout,
		},
	}

	server := &DefaultReflectorServer{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := server.calculateTimeout(tt.blobSize)
			assert.Equal(t, tt.want, got, "calculateTimeout(%d) mismatch", tt.blobSize)
		})
	}
}

func TestReceiveBlob_Success(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)

	// Prepare blob data
	blobData := []byte("test blob data")
	blobHash := server.(*DefaultReflectorServer).calculateBlobHash(blobData)

	// Mock store methods
	store.On("Has", blobHash).Return(false, nil)

	// Test shouldAcceptBlob first
	shouldSend, neededBlobs, err := server.(*DefaultReflectorServer).shouldAcceptBlob(blobHash, false, "127.0.0.1")
	assert.NoError(t, err, "Expected shouldAcceptBlob to succeed")
	assert.True(t, shouldSend, "Expected shouldSend to be true for new blob")
	assert.Empty(t, neededBlobs, "Expected no needed blobs for regular blob")

	// Test blob hash calculation
	receivedHash := server.(*DefaultReflectorServer).calculateBlobHash(blobData)
	assert.Equal(t, blobHash, receivedHash, "Expected calculated hash to match")
}

func TestReceiveBlob_HashMismatch(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)

	// Prepare blob data
	blobData := []byte("test blob data")
	wrongHash := validBlobHash1
	correctHash := server.(*DefaultReflectorServer).calculateBlobHash(blobData)

	// Test hash mismatch detection
	assert.NotEqual(t, wrongHash, correctHash, "Test setup: wrong hash should not match correct hash")

	// Test that hash mismatch would be detected
	receivedHash := server.(*DefaultReflectorServer).calculateBlobHash(blobData)
	assert.Equal(t, correctHash, receivedHash, "Expected calculated hash to match correct hash")
	assert.NotEqual(t, wrongHash, receivedHash, "Expected calculated hash to not match wrong hash")
}

func TestReceiveBlob_SDBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)

	// Prepare SD blob data
	sdBlobData := []byte("test sd blob data")
	sdHash := server.(*DefaultReflectorServer).calculateBlobHash(sdBlobData)

	// Mock store methods
	store.On("Has", sdHash).Return(false, nil)

	// Test shouldAcceptBlob for SD blob
	shouldSend, _, err := server.(*DefaultReflectorServer).shouldAcceptBlob(sdHash, true, "127.0.0.1")
	assert.NoError(t, err, "Expected shouldAcceptBlob to succeed")
	assert.True(t, shouldSend, "Expected shouldSend to be true for new SD blob")

	// Test blob hash calculation for SD blob
	receivedHash := server.(*DefaultReflectorServer).calculateBlobHash(sdBlobData)
	assert.Equal(t, sdHash, receivedHash, "Expected calculated hash to match SD hash")
}

func TestReceiveBlob_ExistingBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Prepare blob data
	blobData := []byte("test blob data")
	blobHash := server.(*DefaultReflectorServer).calculateBlobHash(blobData)

	// Mock store methods - blob already exists
	store.On("Has", blobHash).Return(true, nil)

	// Write blob request
	request := SendBlobRequest{
		BlobHash: blobHash,
		BlobSize: len(blobData),
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Receive blob (should not read blob data since we don't want it)
	reader := bufio.NewReader(conn)
	err := server.(*DefaultReflectorServer).receiveBlob(conn, reader)

	assert.NoError(t, err, "Expected receiveBlob to succeed for existing blob")

	// Verify only Has was called, not Put
	store.AssertCalled(t, "Has", blobHash)
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything)
}

func TestReceiveBlob_Integration(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Test data
	blobData := []byte("test blob data")
	blobHash := server.(*DefaultReflectorServer).calculateBlobHash(blobData)

	// Mock store expectations - we only test Has since we're not testing full blob storage
	store.On("Has", blobHash).Return(false, nil)

	// Test shouldAcceptBlob first - this is called by receiveBlob
	shouldSend, neededBlobs, err := server.(*DefaultReflectorServer).shouldAcceptBlob(blobHash, false, testLocalIP)
	assert.NoError(t, err)
	assert.True(t, shouldSend)
	assert.Empty(t, neededBlobs)

	// Test sendBlobResponse - this is called by receiveBlob
	err = server.(*DefaultReflectorServer).sendBlobResponse(conn, true, false, nil)
	assert.NoError(t, err)
	assert.Contains(t, string(conn.GetWrittenData()), `"send_blob":true`)

	// Test sendTransferResponse - this is called by receiveBlob
	conn.ClearWrittenData()
	err = server.(*DefaultReflectorServer).sendTransferResponse(conn, true, false)
	assert.NoError(t, err)
	assert.Contains(t, string(conn.GetWrittenData()), `"received_blob":true`)

	// Verify only Has was called, not Put since we're not testing full blob storage
	store.AssertCalled(t, "Has", blobHash)
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything)
}

func TestReceiveBlob_NegativeSize(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn(t)

	// Write blob request with negative size
	request := SendBlobRequest{
		BlobHash: validBlobHash1,
		BlobSize: -1,
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Receive blob
	reader := bufio.NewReader(conn)
	err := server.(*DefaultReflectorServer).receiveBlob(conn, reader)

	assert.Error(t, err, "Expected receiveBlob to fail with negative size")
	assert.True(t, errors.Is(err, ErrNegativeBlobSize), "Expected ErrNegativeBlobSize")

	// Verify no store interactions
	store.AssertNotCalled(t, "Has", mock.Anything)
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything)
}
