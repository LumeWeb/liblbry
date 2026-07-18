package chain

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/lbryio/lbcd/btcec"
	"github.com/lbryio/lbcd/chaincfg"
	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/wire"
)

// Input represents an unspent transaction output to spend.
type Input struct {
	TxID         string // hex, internal byte order
	Vout         uint32
	Amount       int64  // satoshis
	Script       []byte // scriptPubKey from prevout (claim script or P2PKH)
}

// Output represents a transaction output to create.
type Output struct {
	Address string
	Amount  int64 // satoshis

	// For claim outputs:
	IsClaim    bool
	ClaimName  string
	ClaimValue []byte // compiled protobuf claim value
	ClaimID    ClaimID // for update_claim and support_claim (zero for claim_name)
	ClaimType  ClaimType
}

// ClaimType identifies the type of claim output script.
type ClaimType byte

const (
	ClaimTypeNone   ClaimType = 0
	ClaimTypeName   ClaimType = 1
	ClaimTypeUpdate ClaimType = 2
	ClaimTypeSupport ClaimType = 3
)

// Builder constructs signed LBRY transactions.
type Builder struct {
	params *chaincfg.Params
}

// NewBuilder creates a transaction builder for the given chain params.
func NewBuilder(params *chaincfg.Params) *Builder {
	return &Builder{params: params}
}

// KeyFunc returns the private key for a given input.
type KeyFunc func(inputIndex int, in Input) (*btcec.PrivateKey, error)

// Build constructs a signed raw transaction from the given inputs and outputs.
func (b *Builder) Build(inputs []Input, outputs []Output, keyFn KeyFunc) (*wire.MsgTx, error) {
	msgTx := wire.NewMsgTx(wire.TxVersion)

	for _, in := range inputs {
		txHash, err := chainhash.NewHashFromStr(in.TxID)
		if err != nil {
			return nil, fmt.Errorf("parse txid %s: %w", in.TxID, err)
		}
		msgTx.AddTxIn(&wire.TxIn{
			PreviousOutPoint: wire.OutPoint{
				Hash:  *txHash,
				Index: in.Vout,
			},
			Sequence: wire.MaxTxInSequenceNum,
		})
	}

	for i, out := range outputs {
		var pkScript []byte
		var err error

		switch {
		case out.IsClaim && out.ClaimType == ClaimTypeName:
			pkScript, err = BuildClaimNameScript(out.ClaimName, out.ClaimValue, out.Address, b.params)
		case out.IsClaim && out.ClaimType == ClaimTypeUpdate:
			pkScript, err = BuildUpdateClaimScript(out.ClaimName, out.ClaimID, out.ClaimValue, out.Address, b.params)
		case out.IsClaim && out.ClaimType == ClaimTypeSupport:
			pkScript, err = BuildSupportClaimScript(out.ClaimName, out.ClaimID, out.ClaimValue, out.Address, b.params)
		case out.IsClaim:
			return nil, fmt.Errorf("build output %d: IsClaim is true but ClaimType is unspecified or unknown", i)
		default:
			pkScript, err = BuildP2PKHScript(out.Address, b.params)
		}
		if err != nil {
			return nil, fmt.Errorf("build output %d script: %w", i, err)
		}

		msgTx.AddTxOut(&wire.TxOut{
			Value:    out.Amount,
			PkScript: pkScript,
		})
	}

	// Sign each input
	for i, in := range inputs {
		privKey, err := keyFn(i, in)
		if err != nil {
			return nil, fmt.Errorf("get key for input %d: %w", i, err)
		}
		signScript := StripClaimScriptPrefix(in.Script)
		sigScript, err := SignatureScript(msgTx, i, signScript, SigHashAll, privKey)
		if err != nil {
			return nil, fmt.Errorf("sign input %d: %w", i, err)
		}
		msgTx.TxIn[i].SignatureScript = sigScript
	}

	return msgTx, nil
}

// Serialize returns the raw transaction bytes.
func Serialize(msgTx *wire.MsgTx) ([]byte, error) {
	var buf bytes.Buffer
	if err := msgTx.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("serialize tx: %w", err)
	}
	return buf.Bytes(), nil
}

// SerializeHex returns the hex-encoded raw transaction.
func SerializeHex(msgTx *wire.MsgTx) (string, error) {
	raw, err := Serialize(msgTx)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// TxID returns the transaction ID (hex, internal byte order).
func TxID(msgTx *wire.MsgTx) string {
	return msgTx.TxHash().String()
}

// EstimateFee calculates the fee for a transaction at the given fee-per-byte rate.
// size is the estimated serialized size in bytes.
func EstimateFee(size int, feePerByte int64) int64 {
	return int64(size) * feePerByte
}

// EstimateTxSize returns a rough size estimate for a transaction with the
// given number of inputs and outputs. LBRY transactions are ~250 bytes per
// input (P2PKH + signature) and ~40 bytes per P2PKH output.
func EstimateTxSize(numInputs, numOutputs int) int {
	return 10 + numInputs*250 + numOutputs*40
}
