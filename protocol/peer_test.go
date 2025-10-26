package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/mocks"
	protocolmocks "go.lumeweb.com/liblbry/protocol/mocks"
)

var blobs = map[string][]byte{
	validBlobHash1: []byte("abcdefg"),
	validBlobHash2: []byte("hijklmn"),
	validBlobHash3: []byte("opqrstu"),
}

type pair struct {
	request  []byte
	response []byte
}

var availabilityRequests = []pair{
	{
		request:  []byte(fmt.Sprintf(`{"requested_blobs":["%s","%s"]}`, validBlobHash1, validBlobHash2)),
		response: []byte(fmt.Sprintf(`{"available_blobs":["%s","%s"]}`, validBlobHash1, validBlobHash2)),
	},
	{
		request:  []byte(fmt.Sprintf(`{"requested_blobs":["%s","%s","yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy"]}`, invalidBlobHash, validBlobHash1)),
		response: []byte(fmt.Sprintf(`{"available_blobs":["%s"]}`, validBlobHash1)),
	},
	{
		request:  []byte(`{"requested_blobs":[]}`),
		response: []byte(emptyAvailableBlobsResponse),
	},
}

var lbrycrdAddressRequests = []pair{
	{
		request:  []byte(`{"lbrycrd_address":true}`),
		response: []byte(fmt.Sprintf(`{"lbrycrd_address":"%s","available_blobs":[]}`, LbrycrdAddress)),
	},
	{
		request:  []byte(fmt.Sprintf(`{"lbrycrd_address":true,"requested_blobs":["%s"]}`, validBlobHash1)),
		response: []byte(fmt.Sprintf(`{"lbrycrd_address":"%s","available_blobs":["%s"]}`, LbrycrdAddress, validBlobHash1)),
	},
}

var blobDataRequests = []pair{
	{
		request:  []byte(fmt.Sprintf(`{"requested_blob":"%s"}`, validBlobHash1)),
		response: []byte(fmt.Sprintf(`{"incoming_blob":{"blob_hash":"%s","length":7},"available_blobs":[]}`, validBlobHash1)),
	},
	{
		request:  []byte(fmt.Sprintf(`{"requested_blob":"%s"}`, invalidBlobHash)),
		response: []byte(fmt.Sprintf(`{"incoming_blob":{"error":"%s","blob_hash":"%s","length":0},"available_blobs":[]}`, ErrBlobNotFound, invalidBlobHash)),
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
		request:  []byte(`{"blob_data_payment_rate":0.0,"requested_blobs":["` + validBlobHash1 + `"]}`),
		response: []byte(`{"available_blobs":["` + validBlobHash1 + `"],"blob_data_payment_rate":"` + PaymentRateAccepted + `"}`),
	},
}

var invalidBlobHashRequests = []pair{
	{
		request:  []byte(`{"requested_blobs":["invalid"]}`),
		response: []byte(emptyAvailableBlobsResponse),
	},
	{
		request:  []byte(fmt.Sprintf(`{"requested_blobs":["%s"]}`, shortBlobHash)),
		response: []byte(emptyAvailableBlobsResponse),
	},
	{
		request:  []byte(`{"requested_blob":"invalid"}`),
		response: []byte(fmt.Sprintf(`{"incoming_blob":{"error":"%s","blob_hash":"invalid","length":0},"available_blobs":[]}`, ErrInvalidHashLen)),
	},
	{
		request:  []byte(fmt.Sprintf(`{"requested_blob":"%s"}`, shortBlobHash)),
		response: []byte(fmt.Sprintf(`{"incoming_blob":{"error":"%s","blob_hash":"%s","length":0},"available_blobs":[]}`, ErrInvalidHashLen, shortBlobHash)),
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

func getServerWithOptions(t *testing.T, withBlobs bool, opts ...ServerOption) (PeerServer, *mocks.MockBlobStore) {
	mockStore := setupMockStore(t, withBlobs)
	return NewPeerServer(mockStore, opts...), mockStore
}

func getServer(t *testing.T, withBlobs bool) (PeerServer, *mocks.MockBlobStore) {
	return getServerWithOptions(t, withBlobs)
}

func getServerWithAccessControl(t *testing.T, withBlobs bool, accessControl liblbry.AccessControl) (PeerServer, *mocks.MockBlobStore) {
	return getServerWithOptions(t, withBlobs, WithAccessControl(accessControl))
}

func getServerWithProtector(t *testing.T, withBlobs bool, protector Protector) (PeerServer, *mocks.MockBlobStore) {
	return getServerWithOptions(t, withBlobs, WithProtector(protector))
}

func getBlobKeys() []string {
	keys := make([]string, 0, len(blobs))
	for k := range blobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func handleRequestAndCompare(t *testing.T, server PeerServer, requestJSON, expectedJSON []byte) {
	t.Helper()
	handleRequestAndCompareWithIP(t, server, requestJSON, expectedJSON, testLocalIP)
}

func handleRequestAndCompareWithIP(t *testing.T, server PeerServer, requestJSON, expectedJSON []byte, peerIP string) {
	t.Helper()
	var req CompositeRequest
	err := json.Unmarshal(requestJSON, &req)
	if err != nil {
		t.Errorf("Failed to unmarshal request: %v", err)
		return
	}

	response, blobData, err := server.(*DefaultPeerServer).handleRequest(req, peerIP)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
		return
	}

	var expectedResponse CompositeResponse
	err = json.Unmarshal(expectedJSON, &expectedResponse)
	if err != nil {
		t.Errorf("Failed to unmarshal expected response: %v", err)
		return
	}

	assert.Equal(t, expectedResponse.LbrycrdAddress, response.LbrycrdAddress)
	assert.Equal(t, expectedResponse.AvailableBlobs, response.AvailableBlobs)
	assert.Equal(t, expectedResponse.BlobDataPaymentRate, response.BlobDataPaymentRate)
	assert.Equal(t, expectedResponse.IncomingBlob, response.IncomingBlob)

	validateBlobData(t, req, response, blobData)
}

// Helper function to unmarshal request with error handling
func unmarshalRequest(t *testing.T, requestJSON []byte) CompositeRequest {
	t.Helper()
	var req CompositeRequest
	err := json.Unmarshal(requestJSON, &req)
	if err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}
	return req
}

// Helper function to unmarshal expected response with error handling
func unmarshalExpectedResponse(t *testing.T, expectedJSON []byte) CompositeResponse {
	t.Helper()
	var expectedResponse CompositeResponse
	err := json.Unmarshal(expectedJSON, &expectedResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal expected response: %v", err)
	}
	return expectedResponse
}

// Helper function to handle request with error handling
func handleTestRequest(t *testing.T, server PeerServer, request CompositeRequest) (CompositeResponse, []byte) {
	t.Helper()
	response, blobData, err := server.(*DefaultPeerServer).handleRequest(request, testLocalIP)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	return response, blobData
}

// Helper function to check blob data
func validateBlobData(t *testing.T, request CompositeRequest, response CompositeResponse, blobData []byte) {
	t.Helper()
	if request.RequestedBlob != "" {
		// If there's an error in the response, we should not expect blob data
		if response.IncomingBlob != nil && response.IncomingBlob.Error != "" {
			// Expect nil blob data for error responses
			if blobData != nil {
				t.Errorf("Expected nil blob data for error response, got data for hash %s", request.RequestedBlob)
			}
			return
		}

		// For successful responses, we expect blob data
		if blobData == nil {
			t.Errorf("Expected blob data for request %s, got nil", request.RequestedBlob)
			return
		}

		// Verify the blob data matches what we expect
		expectedData, exists := blobs[request.RequestedBlob]
		if !exists {
			t.Errorf("Unexpected blob data returned for hash %s", request.RequestedBlob)
		} else if !bytes.Equal(blobData, expectedData) {
			t.Errorf("Blob data mismatch for hash %s. Expected %s, got %s", request.RequestedBlob, string(expectedData), string(blobData))
		}
	}
}

// Helper function to create a CompositeRequest for availability checking
func createAvailabilityRequest(blobHashes []string) CompositeRequest {
	return CompositeRequest{
		RequestedBlobs: blobHashes,
	}
}

// Helper function to create a CompositeRequest for LBRYcrd address
func createLbrycrdAddressRequest(withBlobs bool, blobHashes []string) CompositeRequest {
	req := CompositeRequest{
		LBRYcrdAddress: true,
	}
	if withBlobs {
		req.RequestedBlobs = blobHashes
	}
	return req
}

// Helper function to create a CompositeRequest for blob data
func createBlobDataRequest(blobHash string) CompositeRequest {
	return CompositeRequest{
		RequestedBlob: blobHash,
	}
}

// Helper function to create a CompositeRequest for payment rate
func createPaymentRateRequest(paymentRate float64, blobHashes []string) CompositeRequest {
	return CompositeRequest{
		BlobDataPaymentRate: &paymentRate,
		RequestedBlobs:      blobHashes,
	}
}

// Helper function to create a CompositeResponse with available blobs
func createAvailableBlobsResponse(blobHashes []string) CompositeResponse {
	return CompositeResponse{
		AvailableBlobs: blobHashes,
	}
}

// Helper function to create a CompositeResponse with LBRYcrd address
func createLbrycrdAddressResponse(address string, blobHashes []string) CompositeResponse {
	return CompositeResponse{
		LbrycrdAddress: address,
		AvailableBlobs: blobHashes,
	}
}

// Helper function to create a CompositeResponse with payment rate
func createPaymentRateResponse(rate string, blobHashes []string) CompositeResponse {
	return CompositeResponse{
		BlobDataPaymentRate: rate,
		AvailableBlobs:      blobHashes,
	}
}

// Helper function to create a CompositeResponse with incoming blob
func createIncomingBlobResponse(blobHash string, length int, errorMsg string) CompositeResponse {
	response := CompositeResponse{
		AvailableBlobs: []string{}, // Always initialize as empty slice
	}

	if errorMsg != "" {
		response.IncomingBlob = &IncomingBlob{
			Error:    errorMsg,
			BlobHash: blobHash,
			Length:   0,
		}
	} else {
		response.IncomingBlob = &IncomingBlob{
			BlobHash: blobHash,
			Length:   length,
		}
	}

	return response
}

// Helper function to test a request and compare response
func testRequestAndCompare(t *testing.T, server PeerServer, request CompositeRequest, expectedResponse CompositeResponse) {
	t.Helper()
	response, blobData := handleTestRequest(t, server, request)

	assert.Equal(t, expectedResponse.LbrycrdAddress, response.LbrycrdAddress)
	assert.Equal(t, expectedResponse.AvailableBlobs, response.AvailableBlobs)
	assert.Equal(t, expectedResponse.BlobDataPaymentRate, response.BlobDataPaymentRate)
	assert.Equal(t, expectedResponse.IncomingBlob, response.IncomingBlob)

	validateBlobData(t, request, response, blobData)
}

// Helper function to create a test server with standard options
func setupTestListener(t *testing.T, withBlobs bool) (*mocks.MockBlobStore, net.Listener) {
	t.Helper()
	mockStore := setupMockStore(t, withBlobs)

	listener, err := net.Listen("tcp", testHost)
	if err != nil {
		t.Fatal("Failed to create listener:", err)
	}

	return mockStore, listener
}

// Helper function to start a test server
func startTestServer(listener net.Listener, server PeerServer) {
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.HandleConnection(conn)
		}
	}()
}

// Helper function to test connection requests
func testConnectionRequest(t *testing.T, listener net.Listener, request []byte, expected []byte) {
	t.Helper()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal("error opening connection", err)
	}
	defer func() { _ = conn.Close() }()

	// Set a read deadline to prevent test hanging
	err = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		t.Fatal("error setting read deadline", err)
	}

	response := make([]byte, testBufferSize)
	_, err = conn.Write(request)
	if err != nil {
		t.Fatal("error writing", err)
	}
	n, err := conn.Read(response)
	if err != nil {
		t.Fatal("error reading", err)
	}
	actualResponse := response[:n]
	if !bytes.Equal(actualResponse, expected) {
		t.Errorf("Response did not match expected response.\nExpected: %s\nGot: %s", string(expected), string(actualResponse))
	}
}

func TestAvailabilityRequest_NoBlobs(t *testing.T) {
	s, _ := getServer(t, false)

	expectedResponse := createAvailableBlobsResponse([]string{})

	for _, p := range availabilityRequests {
		req := unmarshalRequest(t, p.request)
		testRequestAndCompare(t, s, req, expectedResponse)
	}
}

func TestLbrycrdAddressRequest(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range lbrycrdAddressRequests {
		handleRequestAndCompare(t, s, p.request, p.response)
	}
}

func TestAvailabilityRequest_WithBlobs(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range availabilityRequests {
		handleRequestAndCompare(t, s, p.request, p.response)
	}
}

func TestBlobDataRequest(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range blobDataRequests {
		handleRequestAndCompare(t, s, p.request, p.response)
	}
}

func TestPaymentRateRequest(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range paymentRateRequests {
		handleRequestAndCompare(t, s, p.request, p.response)
	}
}

func TestInvalidBlobHashes(t *testing.T) {
	s, _ := getServer(t, true)

	for _, p := range invalidBlobHashRequests {
		handleRequestAndCompare(t, s, p.request, p.response)
	}
}

func TestAccessControl(t *testing.T) {
	// Create a mock access control that denies access to the first blob
	mockAccessControl := mocks.NewMockAccessControl(t)
	mockAccessControl.On("Allow", mock.AnythingOfType("string"), testLocalIP).Return(true)
	// Deny access to the first blob in our blobs map
	blobKeys := getBlobKeys()
	if len(blobKeys) > 0 {
		mockAccessControl.On("Allow", blobKeys[0], testDeniedIP).Return(false)
		mockAccessControl.On("Allow", blobKeys[0], testLocalIP).Return(true)
	}

	s, _ := getServerWithAccessControl(t, true, mockAccessControl)

	// Test with allowed IP
	if len(blobKeys) > 0 {
		requestData := []byte(fmt.Sprintf(`{"requested_blobs":["%s"]}`, blobKeys[0]))
		handleRequestAndCompare(t, s, requestData, []byte(fmt.Sprintf(`{"available_blobs":["%s"]}`, blobKeys[0])))
	}

	// Test with denied IP
	if len(blobKeys) > 0 {
		requestData := []byte(fmt.Sprintf(`{"requested_blobs":["%s"]}`, blobKeys[0]))
		expectedResponse := []byte(emptyAvailableBlobsResponse)
		handleRequestAndCompareWithIP(t, s, requestData, expectedResponse, testDeniedIP)
	}
}

func TestProtector(t *testing.T) {
	// Create a mock protector
	mockProtector := protocolmocks.NewMockProtector(t)

	// Get the blob keys
	blobKeys := getBlobKeys()

	// Set up mock behavior - protect the first blob
	if len(blobKeys) > 0 {
		mockProtector.On("IsProtected", blobKeys[0]).Return(true)
		mockProtector.On("IsProtected", mock.AnythingOfType("string")).Return(false)
	}

	s, _ := getServerWithProtector(t, true, mockProtector)

	// Test with protected blob - should not be available
	if len(blobKeys) > 0 {
		// Test availability request
		requestData := []byte(fmt.Sprintf(`{"requested_blobs":["%s"]}`, blobKeys[0]))
		handleRequestAndCompare(t, s, requestData, []byte(emptyAvailableBlobsResponse))

		// Test payment rate request with protected blob
		requestData = []byte(fmt.Sprintf(`{"blob_data_payment_rate":0.0,"requested_blobs":["%s"]}`, blobKeys[0]))
		expectedResponse := []byte(fmt.Sprintf(`{"available_blobs":[],"blob_data_payment_rate":"%s"}`, ErrBlobProtected))
		handleRequestAndCompare(t, s, requestData, expectedResponse)
	}

	// Test with unprotected blob - should be available
	if len(blobKeys) > 1 {
		requestData := []byte(fmt.Sprintf(`{"requested_blobs":["%s"]}`, blobKeys[1]))
		handleRequestAndCompare(t, s, requestData, []byte(fmt.Sprintf(`{"available_blobs":["%s"]}`, blobKeys[1])))
	}
}

func TestRequestFromConnection(t *testing.T) {
	mockStore, listener := setupTestListener(t, true)
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("Error closing listener: %v", err)
		}
	}()

	s := NewPeerServer(mockStore, WithTimeout(10*time.Second))
	startTestServer(listener, s)

	// Test each request
	for _, p := range availabilityRequests {
		testConnectionRequest(t, listener, p.request, p.response)
	}
}

func TestTimeoutHandling(t *testing.T) {
	mockStore, listener := setupTestListener(t, false)
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("Error closing listener: %v", err)
		}
	}()

	s := NewPeerServer(mockStore, WithTimeout(shortTestTimeout))

	startTestServer(listener, s)

	// Connect and don't send anything - should timeout
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Error("error opening connection", err)
	}
	defer func() { _ = conn.Close() }()

	response := make([]byte, testBufferSize)
	_, err = conn.Read(response)
	if err == nil {
		t.Error("Expected timeout error, got none")
	}
}

func TestInvalidDataHandling(t *testing.T) {
	mockStore, listener := setupTestListener(t, false)
	s := NewPeerServer(mockStore)
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("Error closing listener: %v", err)
		}
	}()

	startTestServer(listener, s)

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Error("error opening connection", err)
	}
	defer func() { _ = conn.Close() }()

	response := make([]byte, testBufferSize)
	_, err = conn.Write([]byte("hello dear server, I would like blobs. Please"))
	if err != nil {
		t.Error("error writing", err)
	}

	err = conn.SetReadDeadline(time.Now().Add(testTimeout))
	if err != nil {
		t.Error("error setting read deadline", err)
	}

	_, err = conn.Read(response)
	if err != io.EOF {
		t.Error("error reading", err)
	}
}
