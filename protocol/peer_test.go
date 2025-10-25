package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/mocks"
	protocolmocks "go.lumeweb.com/liblbry/protocol/mocks"
)

var blobs = map[string][]byte{
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef": []byte("abcdefg"),
	"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789": []byte("hijklmn"),
	"123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0": []byte("opqrstu"),
}

type pair struct {
	request  []byte
	response []byte
}

var availabilityRequests = []pair{
	{
		request:  []byte(`{"requested_blobs":["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"]}`),
		response: []byte(`{"available_blobs":["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"]}`),
	},
	{
		request:  []byte(`{"requested_blobs":["ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy"]}`),
		response: []byte(`{"available_blobs":["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"]}`),
	},
	{
		request:  []byte(`{"requested_blobs":[]}`),
		response: []byte(`{"available_blobs":[]}`),
	},
}

// Test constants
const (
	testStoreName = "test"
)

// Test response constants
const (
	emptyAvailableBlobsResponse = `{"available_blobs":[]}`
)

var lbrycrdAddressRequests = []pair{
	{
		request:  []byte(`{"lbrycrd_address":true}`),
		response: []byte(`{"lbrycrd_address":"` + LbrycrdAddress + `","available_blobs":[]}`),
	},
	{
		request:  []byte(`{"lbrycrd_address":true,"requested_blobs":["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"]}`),
		response: []byte(`{"lbrycrd_address":"` + LbrycrdAddress + `","available_blobs":["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"]}`),
	},
}

var blobDataRequests = []pair{
	{
		request:  []byte(`{"requested_blob":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`),
		response: []byte(`{"incoming_blob":{"blob_hash":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","length":7},"available_blobs":[]}`),
	},
	{
		request:  []byte(`{"requested_blob":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}`),
		response: []byte(`{"incoming_blob":{"error":"` + ErrBlobNotFound + `","blob_hash":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","length":0},"available_blobs":[]}`),
	},
}

var paymentRateRequests = []struct {
	request  []byte
	response []byte
}{
	{
		request:  []byte(`{"blob_data_payment_rate":0.0}`),
		response: []byte(`{"blob_data_payment_rate":"` + PaymentRateAccepted + `","available_blobs":[]}`),
	},
	{
		request:  []byte(`{"blob_data_payment_rate":1.0}`),
		response: []byte(`{"blob_data_payment_rate":"` + PaymentRateAccepted + `","available_blobs":[]}`),
	},
	{
		request:  []byte(`{"blob_data_payment_rate":-1.0}`),
		response: []byte(`{"blob_data_payment_rate":"` + PaymentRateTooLow + `","available_blobs":[]}`),
	},
	{
		request:  []byte(`{"blob_data_payment_rate":0.0,"requested_blobs":["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"]}`),
		response: []byte(`{"available_blobs":["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"],"blob_data_payment_rate":"` + PaymentRateAccepted + `"}`),
	},
}

var invalidBlobHashRequests = []pair{
	{
		request:  []byte(`{"requested_blobs":["invalid"]}`),
		response: []byte(`{"available_blobs":[]}`),
	},
	{
		request:  []byte(`{"requested_blobs":["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde"]}`),
		response: []byte(`{"available_blobs":[]}`),
	},
	{
		request:  []byte(`{"requested_blob":"invalid"}`),
		response: []byte(`{"incoming_blob":{"error":"Invalid blob hash length","blob_hash":"invalid","length":0},"available_blobs":[]}`),
	},
	{
		request:  []byte(`{"requested_blob":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde"}`),
		response: []byte(`{"incoming_blob":{"error":"Invalid blob hash length","blob_hash":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde","length":0},"available_blobs":[]}`),
	},
}

func setupMockStore(t *testing.T, withBlobs bool) *mocks.MockBlobStore {
	mockStore := mocks.NewMockBlobStore(t)

	if withBlobs {
		for k, v := range blobs {
			// Set up mock behavior for Has method - use Maybe() to make it flexible
			mockStore.On("Has", k).Maybe().Return(true, nil)
			// Set up mock behavior for Get method - use Maybe() to make it flexible
			mockStore.On("Get", k).Maybe().Return(v, nil)
		}

		// For non-existent blobs, Has should return false
		mockStore.On("Has", mock.AnythingOfType("string")).Maybe().Return(false, nil)
		mockStore.On("Get", mock.AnythingOfType("string")).Maybe().Return([]byte(nil), nil)
	} else {
		mockStore.On("Has", mock.AnythingOfType("string")).Maybe().Return(false, nil)
		mockStore.On("Get", mock.AnythingOfType("string")).Maybe().Return([]byte(nil), nil)
	}

	// Set up mock behavior for Name method - use Maybe() to make it flexible
	mockStore.On("Name").Maybe().Return(testStoreName)

	return mockStore
}

func getServer(t *testing.T, withBlobs bool) (PeerServer, *mocks.MockBlobStore) {
	mockStore := setupMockStore(t, withBlobs)
	return NewPeerServer(mockStore), mockStore
}

func getServerWithAccessControl(t *testing.T, withBlobs bool, accessControl liblbry.AccessControl) (PeerServer, *mocks.MockBlobStore) {
	mockStore := setupMockStore(t, withBlobs)
	return NewPeerServer(mockStore, WithAccessControl(accessControl)), mockStore
}

func getServerWithProtector(t *testing.T, withBlobs bool, protector Protector) (PeerServer, *mocks.MockBlobStore) {
	mockStore := setupMockStore(t, withBlobs)
	return NewPeerServer(mockStore, WithProtector(protector)), mockStore
}

func TestAvailabilityRequest_NoBlobs(t *testing.T) {
	s, _ := getServer(t, false)

	for _, p := range availabilityRequests {
		var req CompositeRequest
		err := json.Unmarshal(p.request, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			continue
		}

		response, _, err := s.(*DefaultPeerServer).handleRequest(req, p.request, "127.0.0.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		responseBytes, err := json.Marshal(response)
		if err != nil {
			t.Errorf("Failed to marshal response: %v", err)
			continue
		}

		if !bytes.Equal(responseBytes, []byte(emptyAvailableBlobsResponse)) {
			t.Errorf("Response did not match expected response. Got %s", string(responseBytes))
		}
	}
}

func TestLbrycrdAddressRequest(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range lbrycrdAddressRequests {
		var req CompositeRequest
		err := json.Unmarshal(p.request, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			continue
		}

		response, _, err := s.(*DefaultPeerServer).handleRequest(req, p.request, "127.0.0.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		// Unmarshal expected response for comparison
		var expectedResponse CompositeResponse
		err = json.Unmarshal(p.response, &expectedResponse)
		if err != nil {
			t.Errorf("Failed to unmarshal expected response: %v", err)
			continue
		}

		// Compare struct fields instead of raw JSON bytes
		assert.Equal(t, expectedResponse.LbrycrdAddress, response.LbrycrdAddress)
		assert.Equal(t, expectedResponse.AvailableBlobs, response.AvailableBlobs)
		assert.Equal(t, expectedResponse.BlobDataPaymentRate, response.BlobDataPaymentRate)
		assert.Equal(t, expectedResponse.IncomingBlob, response.IncomingBlob)
	}
}

func TestAvailabilityRequest_WithBlobs(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range availabilityRequests {
		var req CompositeRequest
		err := json.Unmarshal(p.request, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			continue
		}

		response, _, err := s.(*DefaultPeerServer).handleRequest(req, p.request, "127.0.0.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		responseBytes, err := json.Marshal(response)
		if err != nil {
			t.Errorf("Failed to marshal response: %v", err)
			continue
		}

		if !bytes.Equal(responseBytes, p.response) {
			t.Errorf("Response did not match expected response.\nExpected: %s\nGot: %s", string(p.response), string(responseBytes))
		}
	}
}

func TestBlobDataRequest(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range blobDataRequests {
		var req CompositeRequest
		err := json.Unmarshal(p.request, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			continue
		}

		response, blobData, err := s.(*DefaultPeerServer).handleRequest(req, p.request, "127.0.0.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		// Unmarshal expected response for comparison
		var expectedResponse CompositeResponse
		err = json.Unmarshal(p.response, &expectedResponse)
		if err != nil {
			t.Errorf("Failed to unmarshal expected response: %v", err)
			continue
		}

		// Compare struct fields instead of raw JSON bytes
		assert.Equal(t, expectedResponse.LbrycrdAddress, response.LbrycrdAddress)
		assert.Equal(t, expectedResponse.AvailableBlobs, response.AvailableBlobs)
		assert.Equal(t, expectedResponse.BlobDataPaymentRate, response.BlobDataPaymentRate)
		assert.Equal(t, expectedResponse.IncomingBlob, response.IncomingBlob)

		// Check blob data for successful requests
		if req.RequestedBlob != "" && blobData == nil {
			// For non-existent blobs, we expect nil data
			if response.IncomingBlob != nil && response.IncomingBlob.Error == ErrBlobNotFound {
				continue
			}
			t.Errorf("Expected blob data for request %s, got nil", req.RequestedBlob)
		} else if req.RequestedBlob != "" && blobData != nil {
			// Verify the blob data matches what we expect
			expectedData, exists := blobs[req.RequestedBlob]
			if !exists {
				t.Errorf("Unexpected blob data returned for hash %s", req.RequestedBlob)
			} else if !bytes.Equal(blobData, expectedData) {
				t.Errorf("Blob data mismatch for hash %s. Expected %s, got %s", req.RequestedBlob, string(expectedData), string(blobData))
			}
		}
	}
}

func TestPaymentRateRequest(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range paymentRateRequests {
		var req CompositeRequest
		err := json.Unmarshal(p.request, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			continue
		}

		response, _, err := s.(*DefaultPeerServer).handleRequest(req, p.request, "127.0.0.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		// Unmarshal expected response for comparison
		var expectedResponse CompositeResponse
		err = json.Unmarshal(p.response, &expectedResponse)
		if err != nil {
			t.Errorf("Failed to unmarshal expected response: %v", err)
			continue
		}

		// Compare struct fields instead of raw JSON bytes
		assert.Equal(t, expectedResponse.LbrycrdAddress, response.LbrycrdAddress)
		assert.Equal(t, expectedResponse.AvailableBlobs, response.AvailableBlobs)
		assert.Equal(t, expectedResponse.BlobDataPaymentRate, response.BlobDataPaymentRate)
		assert.Equal(t, expectedResponse.IncomingBlob, response.IncomingBlob)
	}
}

func TestInvalidBlobHashes(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range invalidBlobHashRequests {
		var req CompositeRequest
		err := json.Unmarshal(p.request, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			continue
		}

		response, _, err := s.(*DefaultPeerServer).handleRequest(req, p.request, "127.0.0.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		// Unmarshal expected response for comparison
		var expectedResponse CompositeResponse
		err = json.Unmarshal(p.response, &expectedResponse)
		if err != nil {
			t.Errorf("Failed to unmarshal expected response: %v", err)
			continue
		}

		// Compare struct fields instead of raw JSON bytes
		assert.Equal(t, expectedResponse.LbrycrdAddress, response.LbrycrdAddress)
		assert.Equal(t, expectedResponse.AvailableBlobs, response.AvailableBlobs)
		assert.Equal(t, expectedResponse.BlobDataPaymentRate, response.BlobDataPaymentRate)
		assert.Equal(t, expectedResponse.IncomingBlob, response.IncomingBlob)
	}
}

func TestAccessControl(t *testing.T) {
	// Create a mock access control that denies access to the first blob
	mockAccessControl := mocks.NewMockAccessControl(t)
	mockAccessControl.On("Allow", mock.AnythingOfType("string"), "127.0.0.1").Return(true)
	// Deny access to the first blob in our blobs map
	blobKeys := make([]string, 0, len(blobs))
	for k := range blobs {
		blobKeys = append(blobKeys, k)
	}
	if len(blobKeys) > 0 {
		mockAccessControl.On("Allow", blobKeys[0], "192.168.1.1").Return(false)
		mockAccessControl.On("Allow", blobKeys[0], "127.0.0.1").Return(true)
	}

	s, _ := getServerWithAccessControl(t, true, mockAccessControl)

	// Test with allowed IP
	if len(blobKeys) > 0 {
		requestData := []byte(`{"requested_blobs":["` + blobKeys[0] + `"]}`)
		var req CompositeRequest
		err := json.Unmarshal(requestData, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			return
		}

		response, _, err := s.(*DefaultPeerServer).handleRequest(req, requestData, "127.0.0.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		responseBytes, err := json.Marshal(response)
		if err != nil {
			t.Errorf("Failed to marshal response: %v", err)
			return
		}

		expectedResponse := []byte(`{"available_blobs":["` + blobKeys[0] + `"]}`)
		if !bytes.Equal(responseBytes, expectedResponse) {
			t.Errorf("Response did not match expected response.\nExpected: %s\nGot: %s", string(expectedResponse), string(responseBytes))
		}
	}

	// Test with denied IP
	if len(blobKeys) > 0 {
		requestData := []byte(`{"requested_blobs":["` + blobKeys[0] + `"]}`)
		var req CompositeRequest
		err := json.Unmarshal(requestData, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			return
		}

		response, _, err := s.(*DefaultPeerServer).handleRequest(req, requestData, "192.168.1.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		responseBytes, err := json.Marshal(response)
		if err != nil {
			t.Errorf("Failed to marshal response: %v", err)
			return
		}

		expectedResponse := []byte(`{"available_blobs":[]}`)
		if !bytes.Equal(responseBytes, expectedResponse) {
			t.Errorf("Response did not match expected response.\nExpected: %s\nGot: %s", string(expectedResponse), string(responseBytes))
		}
	}
}

func TestProtector(t *testing.T) {
	// Create a mock protector
	mockProtector := protocolmocks.NewMockProtector(t)

	// Get the first blob key to protect
	blobKeys := make([]string, 0, len(blobs))
	for k := range blobs {
		blobKeys = append(blobKeys, k)
	}

	// Set up mock behavior - protect the first blob
	if len(blobKeys) > 0 {
		mockProtector.On("IsProtected", blobKeys[0]).Return(true)
		mockProtector.On("IsProtected", mock.AnythingOfType("string")).Return(false)
	}

	s, _ := getServerWithProtector(t, true, mockProtector)

	// Test with protected blob - should not be available
	if len(blobKeys) > 0 {
		requestData := []byte(`{"requested_blobs":["` + blobKeys[0] + `"]}`)
		var req CompositeRequest
		err := json.Unmarshal(requestData, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			return
		}

		response, _, err := s.(*DefaultPeerServer).handleRequest(req, requestData, "127.0.0.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		responseBytes, err := json.Marshal(response)
		if err != nil {
			t.Errorf("Failed to marshal response: %v", err)
			return
		}

		expectedResponse := []byte(`{"available_blobs":[]}`)
		if !bytes.Equal(responseBytes, expectedResponse) {
			t.Errorf("Response did not match expected response.\nExpected: %s\nGot: %s", string(expectedResponse), string(responseBytes))
		}
	}

	// Test with unprotected blob - should be available
	if len(blobKeys) > 1 {
		requestData := []byte(`{"requested_blobs":["` + blobKeys[1] + `"]}`)
		var req CompositeRequest
		err := json.Unmarshal(requestData, &req)
		if err != nil {
			t.Errorf("Failed to unmarshal request: %v", err)
			return
		}

		response, _, err := s.(*DefaultPeerServer).handleRequest(req, requestData, "127.0.0.1")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		responseBytes, err := json.Marshal(response)
		if err != nil {
			t.Errorf("Failed to marshal response: %v", err)
			return
		}

		expectedResponse := []byte(`{"available_blobs":["` + blobKeys[1] + `"]}`)
		if !bytes.Equal(responseBytes, expectedResponse) {
			t.Errorf("Response did not match expected response.\nExpected: %s\nGot: %s", string(expectedResponse), string(responseBytes))
		}
	}
}

func TestRequestFromConnection(t *testing.T) {
	mockStore := setupMockStore(t, true)
	s := NewPeerServer(mockStore, WithTimeout(10*time.Second))

	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("Failed to create listener:", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.HandleConnection(conn)
		}
	}()

	// Test each request
	for _, p := range availabilityRequests {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Error("error opening connection", err)
		}
		defer func() { _ = conn.Close() }()

		response := make([]byte, 8192)
		_, err = conn.Write(p.request)
		if err != nil {
			t.Error("error writing", err)
		}
		_, err = conn.Read(response)
		if err != nil {
			t.Error("error reading", err)
		}

		// Find the end of the JSON response
		end := bytes.Index(response, []byte{0})
		if end == -1 {
			end = len(response)
		}

		// Trim any null bytes and compare
		actualResponse := bytes.Trim(response[:end], "\x00")
		if !bytes.Equal(actualResponse, p.response) {
			t.Errorf("Response did not match expected response.\nExpected: %s\nGot: %s", string(p.response), string(actualResponse))
		}
	}
}

func TestTimeoutHandling(t *testing.T) {
	mockStore := mocks.NewMockBlobStore(t)
	mockStore.On("Name").Maybe().Return("test")

	// Create server with very short timeout
	s := NewPeerServer(mockStore, WithTimeout(1*time.Millisecond))

	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("Failed to create listener:", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.HandleConnection(conn)
		}
	}()

	// Connect and don't send anything - should timeout
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Error("error opening connection", err)
	}
	defer func() { _ = conn.Close() }()

	response := make([]byte, 8192)
	_, err = conn.Read(response)
	if err == nil {
		t.Error("Expected timeout error, got none")
	}
}

func TestInvalidDataHandling(t *testing.T) {
	mockStore := setupMockStore(t, false)
	s := NewPeerServer(mockStore, WithTimeout(5*time.Second))

	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("Failed to create listener:", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.HandleConnection(conn)
		}
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Error("error opening connection", err)
	}
	defer func() { _ = conn.Close() }()

	response := make([]byte, 8192)
	_, err = conn.Write([]byte("hello dear server, I would like blobs. Please"))
	if err != nil {
		t.Error("error writing", err)
	}

	err = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		t.Error("error setting read deadline", err)
	}

	_, err = conn.Read(response)
	if err != io.EOF {
		t.Error("error reading", err)
	}
}

func TestInvalidData(t *testing.T) {
	mockStore := setupMockStore(t, false)
	s := NewPeerServer(mockStore, WithTimeout(5*time.Second))

	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("Failed to create listener:", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.HandleConnection(conn)
		}
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Error("error opening connection", err)
	}
	defer func() { _ = conn.Close() }()

	response := make([]byte, 8192)
	_, err = conn.Write([]byte("hello dear server, I would like blobs. Please"))
	if err != nil {
		t.Error("error writing", err)
	}

	err = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		t.Error("error setting read deadline", err)
	}

	_, err = conn.Read(response)
	if err != io.EOF {
		t.Error("error reading", err)
	}
}
