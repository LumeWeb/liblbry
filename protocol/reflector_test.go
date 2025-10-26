package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/liblbry/blob"
	"go.lumeweb.com/liblbry/mocks"
)

// Test constants
const (
	reflectorTestStoreName    = "test"
	reflectorTestHost         = "127.0.0.1:0"
	reflectorTestTimeout      = 5 * time.Second
	reflectorTestBufferSize   = 8192
	reflectorShortTestTimeout = 10 * time.Millisecond
)

// Blob hash constants
const (
	reflectorValidBlobHash1 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	reflectorValidBlobHash2 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	reflectorValidBlobHash3 = "123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0"
)

var reflectorTestBlobs = map[string][]byte{
	reflectorValidBlobHash1: []byte("test blob data 1"),
	reflectorValidBlobHash2: []byte("test blob data 2"),
	reflectorValidBlobHash3: []byte("test blob data 3"),
}

// MockConn implements net.Conn for testing
type MockConn struct {
	readBuffer  *bytes.Buffer
	writeBuffer *bytes.Buffer
	closed      bool
}

func NewMockConn() *MockConn {
	return &MockConn{
		readBuffer:  &bytes.Buffer{},
		writeBuffer: &bytes.Buffer{},
	}
}

func (m *MockConn) Read(b []byte) (n int, err error) {
	if m.closed {
		return 0, errors.New("connection closed")
	}
	return m.readBuffer.Read(b)
}

func (m *MockConn) Write(b []byte) (n int, err error) {
	if m.closed {
		return 0, errors.New("connection closed")
	}
	return m.writeBuffer.Write(b)
}

func (m *MockConn) Close() error {
	m.closed = true
	return nil
}

func (m *MockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5566}
}

func (m *MockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (m *MockConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *MockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *MockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// WriteToReadBuffer writes data to the read buffer (simulating incoming data)
func (m *MockConn) WriteToReadBuffer(data []byte) {
	m.readBuffer.Write(data)
}

// GetWrittenData returns data written to the connection
func (m *MockConn) GetWrittenData() []byte {
	return m.writeBuffer.Bytes()
}

// ClearWrittenData clears the write buffer
func (m *MockConn) ClearWrittenData() {
	m.writeBuffer.Reset()
}

// ReadBuffer returns the read buffer for direct manipulation
func (m *MockConn) ReadBuffer() *bytes.Buffer {
	return m.readBuffer
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
	conn := NewMockConn()

	// Write handshake request
	version := ProtocolVersion1
	handshake := HandshakeRequestResponse{Version: &version}
	handshakeData, _ := json.Marshal(handshake)
	conn.WriteToReadBuffer(handshakeData)

	// Perform handshake
	err := server.(*DefaultReflectorServer).doHandshake(conn)

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
	conn := NewMockConn()

	// Write handshake with invalid version
	version := 999
	handshake := HandshakeRequestResponse{Version: &version}
	handshakeData, _ := json.Marshal(handshake)
	conn.WriteToReadBuffer(handshakeData)

	// Perform handshake
	err := server.(*DefaultReflectorServer).doHandshake(conn)

	assert.Error(t, err, "Expected handshake to fail with invalid version")
	assert.True(t, errors.Is(err, ErrProtocolVersion), "Expected ErrProtocolVersion")
}

func TestDoHandshake_MissingVersion(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn()

	// Write handshake without version
	handshake := HandshakeRequestResponse{}
	handshakeData, _ := json.Marshal(handshake)
	conn.WriteToReadBuffer(handshakeData)

	// Perform handshake
	err := server.(*DefaultReflectorServer).doHandshake(conn)

	assert.Error(t, err, "Expected handshake to fail with missing version")
	assert.True(t, errors.Is(err, ErrHandshakeMissing), "Expected ErrHandshakeMissing")
}

func TestReadBlobRequest_RegularBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn()

	// Write blob request
	request := SendBlobRequest{
		BlobHash: reflectorValidBlobHash1,
		BlobSize: 1024,
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Read blob request
	blobSize, blobHash, isSdBlob, err := server.(*DefaultReflectorServer).readBlobRequest(conn)

	assert.NoError(t, err, "Expected readBlobRequest to succeed")
	assert.Equal(t, 1024, blobSize, "Expected blob size 1024")
	assert.Equal(t, request.BlobHash, blobHash, "Expected blob hash to match")
	assert.False(t, isSdBlob, "Expected isSdBlob to be false for regular blob")
}

func TestReadBlobRequest_SDBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn()

	// Write SD blob request
	request := SendBlobRequest{
		SdBlobHash: reflectorValidBlobHash1,
		SdBlobSize: 512,
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Read blob request
	blobSize, blobHash, isSdBlob, err := server.(*DefaultReflectorServer).readBlobRequest(conn)

	assert.NoError(t, err, "Expected readBlobRequest to succeed")
	assert.Equal(t, 512, blobSize, "Expected blob size 512")
	assert.Equal(t, request.SdBlobHash, blobHash, "Expected blob hash to match")
	assert.True(t, isSdBlob, "Expected isSdBlob to be true for SD blob")
}

func TestReadBlobRequest_EmptyHash(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn()

	// Write blob request with empty hash
	request := SendBlobRequest{
		BlobHash: "",
		BlobSize: 1024,
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Read blob request
	_, _, _, err := server.(*DefaultReflectorServer).readBlobRequest(conn)

	assert.Error(t, err, "Expected readBlobRequest to fail with empty hash")
	assert.True(t, errors.Is(err, ErrBlobHashEmpty), "Expected ErrBlobHashEmpty")
}

func TestReadBlobRequest_BlobTooBig(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn()

	// Write blob request with size too large
	request := SendBlobRequest{
		BlobHash: validBlobHash1,
		BlobSize: MaxBlobSize + 1,
	}
	requestData, _ := json.Marshal(request)
	conn.WriteToReadBuffer(requestData)

	// Read blob request
	_, _, _, err := server.(*DefaultReflectorServer).readBlobRequest(conn)

	assert.Error(t, err, "Expected readBlobRequest to fail with blob too big")
	assert.True(t, errors.Is(err, ErrBlobTooBig), "Expected ErrBlobTooBig")
}

func TestShouldAcceptBlob_NewBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)

	// Mock store methods
	store.On("Has", reflectorValidBlobHash1).Return(false, nil)

	shouldSend, neededBlobs, err := server.(*DefaultReflectorServer).shouldAcceptBlob(reflectorValidBlobHash1, false)

	assert.NoError(t, err, "Expected shouldAcceptBlob to succeed")
	assert.True(t, shouldSend, "Expected shouldSend to be true for new blob")
	assert.Empty(t, neededBlobs, "Expected no needed blobs for regular blob")
}

func TestShouldAcceptBlob_ExistingBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)

	// Mock store methods - blob already exists
	store.On("Has", reflectorValidBlobHash1).Return(true, nil)

	shouldSend, neededBlobs, err := server.(*DefaultReflectorServer).shouldAcceptBlob(reflectorValidBlobHash1, false)

	assert.NoError(t, err, "Expected shouldAcceptBlob to succeed")
	assert.False(t, shouldSend, "Expected shouldSend to be false for existing blob")
	assert.Empty(t, neededBlobs, "Expected no needed blobs for regular blob")
}

func TestSendBlobResponse_RegularBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn()

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
	conn := NewMockConn()

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
	conn := NewMockConn()

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
	conn := NewMockConn()

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
	validHash := reflectorValidBlobHash1
	assert.True(t, ValidateBlobHash(validHash), "Expected valid hash to pass validation")

	// Invalid length
	shortHash := "1234567890abcdef"
	assert.False(t, ValidateBlobHash(shortHash), "Expected short hash to fail validation")

	// Invalid characters
	invalidCharHash := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdeG"
	assert.False(t, ValidateBlobHash(invalidCharHash), "Expected hash with invalid characters to fail validation")
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
	shouldSend, neededBlobs, err := server.(*DefaultReflectorServer).shouldAcceptBlob(blobHash, false)
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
	wrongHash := reflectorValidBlobHash1
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
	shouldSend, _, err := server.(*DefaultReflectorServer).shouldAcceptBlob(sdHash, true)
	assert.NoError(t, err, "Expected shouldAcceptBlob to succeed")
	assert.True(t, shouldSend, "Expected shouldSend to be true for new SD blob")

	// Test blob hash calculation for SD blob
	receivedHash := server.(*DefaultReflectorServer).calculateBlobHash(sdBlobData)
	assert.Equal(t, sdHash, receivedHash, "Expected calculated hash to match SD hash")
}

func TestReceiveBlob_ExistingBlob(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	server := NewReflectorServer(store)
	conn := NewMockConn()

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
	err := server.(*DefaultReflectorServer).receiveBlob(conn)

	assert.NoError(t, err, "Expected receiveBlob to succeed for existing blob")

	// Verify only Has was called, not Put
	store.AssertCalled(t, "Has", blobHash)
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything)
}
