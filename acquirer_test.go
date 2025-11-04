package liblbry

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/blob/transfer"
	transferMocks "go.lumeweb.com/liblbry/blob/transfer/mocks"
	lbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/storage/mocks"
)

func TestNewBlobAcquirer(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transfers := []transfer.Transfer{}

	acquirer := NewBlobAcquirer(transfers, store)

	assert.NotNil(t, acquirer)
	// Test that the returned value implements the BlobAcquirer interface
	_, ok := acquirer.(BlobAcquirer)
	assert.True(t, ok)
}

func TestBlobAcquirer_Acquire_FromStorage(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transfers := []transfer.Transfer{}

	hash := "testhash123"
	expectedData := []byte("test data from storage")

	store.EXPECT().Has(hash).Return(true, nil)
	store.EXPECT().Get(hash).Return(expectedData, nil)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.NoError(t, err)
	assert.Equal(t, expectedData, data)
}

func TestBlobAcquirer_Acquire_FromTransfer(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transferMock := transferMocks.NewMockTransfer(t)
	transfers := []transfer.Transfer{transferMock}

	hash := "testhash123"
	expectedData := []byte("test data from transfer")

	store.EXPECT().Has(hash).Return(false, nil)
	transferMock.EXPECT().Get(hash).Return(expectedData, nil)
	store.EXPECT().Put(hash, expectedData).Return(nil)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.NoError(t, err)
	assert.Equal(t, expectedData, data)
}

func TestBlobAcquirer_Acquire_TransferFallback(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transferMock1 := transferMocks.NewMockTransfer(t)
	transferMock2 := transferMocks.NewMockTransfer(t)
	transfers := []transfer.Transfer{transferMock1, transferMock2}

	hash := "testhash123"
	expectedData := []byte("test data from second transfer")

	store.EXPECT().Has(hash).Return(false, nil)
	transferMock1.EXPECT().Get(hash).Return(nil, errors.New("first transfer failed"))
	transferMock2.EXPECT().Get(hash).Return(expectedData, nil)
	store.EXPECT().Put(hash, expectedData).Return(nil)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.NoError(t, err)
	assert.Equal(t, expectedData, data)
}

func TestBlobAcquirer_Acquire_AllMethodsFail(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transferMock1 := transferMocks.NewMockTransfer(t)
	transferMock2 := transferMocks.NewMockTransfer(t)
	transfers := []transfer.Transfer{transferMock1, transferMock2}

	hash := "testhash123"

	store.EXPECT().Has(hash).Return(false, nil)
	transferMock1.EXPECT().Get(hash).Return(nil, errors.New("first transfer failed"))
	transferMock2.EXPECT().Get(hash).Return(nil, errors.New("second transfer failed"))

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, lbryerrors.ErrAcquisitionFailed, err)
}

func TestBlobAcquirer_Acquire_StorageHasError(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transfers := []transfer.Transfer{}

	hash := "testhash123"
	expectedError := errors.New("storage has error")

	store.EXPECT().Has(hash).Return(false, expectedError)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, expectedError, err)
}

func TestBlobAcquirer_Acquire_StorageGetError(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transfers := []transfer.Transfer{}

	hash := "testhash123"
	expectedError := errors.New("storage get error")

	store.EXPECT().Has(hash).Return(true, nil)
	store.EXPECT().Get(hash).Return(nil, expectedError)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, expectedError, err)
}

func TestBlobAcquirer_Acquire_EmptyTransfersList(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transfers := []transfer.Transfer{}

	hash := "testhash123"

	store.EXPECT().Has(hash).Return(false, nil)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, lbryerrors.ErrAcquisitionFailed, err)
}

func TestBlobAcquirer_Acquire_StoragePutError(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transferMock := transferMocks.NewMockTransfer(t)
	transfers := []transfer.Transfer{transferMock}

	hash := "testhash123"
	transferData := []byte("test data from transfer")
	expectedError := errors.New("storage put error")

	store.EXPECT().Has(hash).Return(false, nil)
	transferMock.EXPECT().Get(hash).Return(transferData, nil)
	store.EXPECT().Put(hash, transferData).Return(expectedError)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, expectedError, err)
}

func TestBlobAcquirer_Acquire_TransferNameCalled(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transferMock := transferMocks.NewMockTransfer(t)
	transfers := []transfer.Transfer{transferMock}

	hash := "testhash123"
	expectedData := []byte("test data from transfer")

	store.EXPECT().Has(hash).Return(false, nil)
	transferMock.EXPECT().Get(hash).Return(expectedData, nil)
	store.EXPECT().Put(hash, expectedData).Return(nil)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.NoError(t, err)
	assert.Equal(t, expectedData, data)
}

func TestBlobAcquirer_Acquire_EmptyHash(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transfers := []transfer.Transfer{}

	hash := ""

	store.EXPECT().Has(hash).Return(false, nil)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.Error(t, err)
	assert.Nil(t, data)
}

func TestBlobAcquirer_Acquire_EmptyDataFromTransfer(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transferMock := transferMocks.NewMockTransfer(t)
	transfers := []transfer.Transfer{transferMock}

	hash := "testhash123"
	expectedData := []byte{} // Empty byte array

	store.EXPECT().Has(hash).Return(false, nil)
	transferMock.EXPECT().Get(hash).Return(expectedData, nil)
	store.EXPECT().Put(hash, expectedData).Return(nil)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.NoError(t, err)
	assert.Equal(t, expectedData, data)
}

func TestBlobAcquirer_Acquire_NilDataFromTransfer(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transferMock := transferMocks.NewMockTransfer(t)
	transfers := []transfer.Transfer{transferMock}

	hash := "testhash123"

	store.EXPECT().Has(hash).Return(false, nil)
	transferMock.EXPECT().Get(hash).Return(nil, nil) // Nil data but no error
	store.EXPECT().Put(hash, []byte(nil)).Return(nil)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.NoError(t, err)
	assert.Equal(t, []byte(nil), data)
}

func TestBlobAcquirer_Acquire_MultipleTransfersMixedResults(t *testing.T) {
	store := mocks.NewMockBlobStore(t)
	transferMock1 := transferMocks.NewMockTransfer(t)
	transferMock2 := transferMocks.NewMockTransfer(t)
	transferMock3 := transferMocks.NewMockTransfer(t)
	transfers := []transfer.Transfer{transferMock1, transferMock2, transferMock3}

	hash := "testhash123"
	expectedData := []byte{} // Empty data from transfer2

	store.EXPECT().Has(hash).Return(false, nil)
	transferMock1.EXPECT().Get(hash).Return(nil, errors.New("first transfer failed"))
	transferMock2.EXPECT().Get(hash).Return([]byte{}, nil) // Empty data
	store.EXPECT().Put(hash, []byte{}).Return(nil)

	acquirer := NewBlobAcquirer(transfers, store)

	data, err := acquirer.Acquire(hash)

	require.NoError(t, err)
	assert.Equal(t, expectedData, data)
}
