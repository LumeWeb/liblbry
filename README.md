# liblbry

A Go library for LBRY protocol implementation: HD wallets, chain types, claim management, blob transfers, and Electrum protocol support.

## Overview

liblbry is a comprehensive Go library for the LBRY protocol. It provides everything needed to build LBRY applications: HD wallet key management, transaction and claim script building, protobuf claim serialization, coin selection, and Electrum JSON-RPC protocol support. Historically focused on blob storage and P2P streaming, it has evolved into a full protocol implementation.

## Features

- **HD Wallet** (`wallet/`): BIP39 mnemonics, BIP32 key derivation, address gap scanning
- **Chain Types** (`chain/`): Claim ID generation, LBRY script builders, transaction construction
- **Claim Management** (`claim/`): Protobuf claim builders (stream, channel, repost, collection), channel signing with secp256k1 compact signatures
- **Coin Selection** (`coinselect/`): BnB and best-match algorithms for UTXO selection
- **Electrum Protocol** (`electrum/`): JSON-RPC client types and script-hash monitoring
- **Blob Management**: Handle blob storage with memory and disk backends
- **Transfer Mechanisms**: Peer-to-peer blob transfer with DHT integration
- **Stream Handling**: SD blob management and stream acquisition
- **Storage Interfaces**: Flexible storage abstractions with access control
- **Cryptographic Utilities**: SHA-384 hashing and encryption support

## Architecture

The library follows a component-based architecture organized into several key modules:

### Core Components

1. **Wallet** (`wallet/`)
   - BIP39 mnemonic seed generation with Electrum wordlist
   - BIP32 hierarchical deterministic key derivation using btcec
   - LBRY P2PKH address encoding and gap-limit address scanning

2. **Chain** (`chain/`)
   - Claim ID derivation from outpoints (RIPEMD160(SHA256(txid || vout)))
   - LBRY claim script builders (claim name, update, support)
   - Transaction builder with signature hash support
   - Cross-verification tests against lbcd consensus

3. **Claim** (`claim/`)
   - Protobuf claim builders: Stream, Channel, Repost, Collection
   - Channel claim signing with secp256k1 compact signatures
   - Low-S normalization for malleability resistance
   - Claim value parser (unsigned and signed round-trip)

4. **Coin Selection** (`coinselect/`)
   - Branch-and-Bound (BnB) exact-match algorithm
   - Closest-match fallback with waste minimization
   - Fee estimation and effective-value sorting

5. **Electrum** (`electrum/`)
   - JSON-RPC method types for ElectrumX protocol
   - Script-hash balance and history monitoring
   - Generic Client interface for TCP, WebSocket, or HTTP transports

6. **Blob Management** (`blob/`)
   - Blob structures and encryption/decryption
   - Blob storage interfaces and implementations (memory, disk)
   - Blob transfer mechanisms (peer-to-peer, reflector)

7. **Storage Layer** (`storage/`)
   - BlobStore interface defining storage operations
   - Memory and disk storage implementations
   - Access control mechanisms

8. **Transfer Layer** (`blob/transfer/`)
   - Generic Transfer interface for blob acquisition
   - Peer transfer implementation with DHT discovery
   - Configurable transfer options

9. **Stream Handling** (`stream/`)
   - Stream metadata and SD blob handling
   - Hash type definitions
   - Stream acquisition and reading

10. **Protocol Layer** (`protocol/`)
   - DHT (Distributed Hash Table) integration
   - Peer protocol handling
   - Reflector protocol handling
   - Network communication components

11. **Server Layer** (`server/`)
   - Server builder with fluent API
   - Protocol configuration (peer, reflector, DHT)
   - Blob management and acquisition

12. **Client Layer** (`client/`)
   - Stream acquisition with memory-efficient streaming
   - Blob acquisition with retry logic
   - SD blob handling and content retrieval

## Key Design Patterns

1. **Component-Based Architecture**: The peer transfer uses a component-based approach with separate modules for discovery, connection management, coordination, and downloading.

2. **Interface Segregation**: Clear separation of concerns with well-defined interfaces (BlobStore, Transfer, etc.)

3. **Configuration Flexibility**: Transfer options and server builder provide extensive customization capabilities

4. **Error Handling**: Comprehensive error handling with specific error types and retry mechanisms

5. **Logging**: Structured logging throughout the system using zap.Logger

## Modules

- `wallet/`: HD wallet (BIP39/BIP32), address management, gap scanning
- `chain/`: LBRY chain types — claim IDs, scripts, transaction builder
- `claim/`: Protobuf claim builders and signing (stream, channel, repost, collection)
- `coinselect/`: UTXO coin selection (BnB, best-match)
- `electrum/`: ElectrumX JSON-RPC protocol types and client interface
- `blob/`: Blob handling and encryption
- `storage/`: Storage interfaces and implementations
- `stream/`: Stream and SD blob handling
- `protocol/`: Network protocols (DHT, peer, reflector)
- `server/`: Server configuration and management
- `client/`: Client-side stream acquisition
- `blob/transfer/peer_transfer/`: Peer-to-peer blob transfer implementation
- `crypto/`: Cryptographic utilities
- `errors/`: Custom error types
- `internal/`: Internal utilities and testing helpers

## Installation

```bash
go get go.lumeweb.com/liblbry
```

## Usage Examples

### Basic Server Setup

```go
package main

import (
    "go.lumeweb.com/liblbry/server"
    "go.lumeweb.com/liblbry/storage/memory"
    "go.uber.org/zap"
)

func main() {
    // Create a memory storage backend
    storage := memory.NewMemoryStore()
    
    // Build a server with default configuration
    builder := server.NewServerBuilder().
        WithStorage(storage).
        WithDefaultAcquirer()
    
    // Create the server
    // Note: zap.NewDevelopment() is used for examples. Use zap.NewProduction() in production
    srv, err := builder.Build(zap.NewDevelopment())
    if err != nil {
        panic(err)
    }
    
    // Start the server
    err = srv.Start()
    if err != nil {
        panic(err)
    }
    
    // Keep the server running
    select {}
}
```

### Stream Acquisition

```go
package main

import (
    "context"
    "fmt"
    "go.lumeweb.com/liblbry"
    "go.lumeweb.com/liblbry/blob/transfer"
    "go.lumeweb.com/liblbry/blob/transfer/peer_transfer"
    "go.lumeweb.com/liblbry/client"
    "go.lumeweb.com/liblbry/protocol"
    "go.lumeweb.com/liblbry/storage/memory"
    "go.uber.org/zap"
)

func main() {
    // Create a memory storage backend
    storage := memory.NewMemoryStore()
    
    // Create a blob transfer (using peer transfer as an example)
    // Note: In a real application, you'd configure proper DHT node and peer client factory
    dhtNode, err := protocol.NewDHTNodeWithDefaults()
    if err != nil {
        panic(fmt.Sprintf("Failed to create DHT node: %v", err))
    }
    
    peerTransfer, err := peer_transfer.NewPeerTransfer(dhtNode, nil) // peer client factory can be nil for basic usage
    if err != nil {
        panic(fmt.Sprintf("Failed to create peer transfer: %v", err))
    }
    
    // Create a blob acquirer with the transfer methods
    blobAcquirer, err := liblbry.NewBlobAcquirer([]transfer.Transfer{peerTransfer}, storage)
    if err != nil {
        panic(fmt.Sprintf("Failed to create blob acquirer: %v", err))
    }
    
    // Create a stream acquirer with all required parameters
    // Note: zap.NewDevelopment() is used for examples. Use zap.NewProduction() in production
    streamAcquirer := client.NewStreamAcquirer(blobAcquirer, storage, zap.NewDevelopment())
    
    // Acquire a stream (using a sample SD blob hash)
    ctx := context.Background()
    sdHash := "sample_sd_blob_hash"
    
    // Get stream result (loads all content into memory)
    result, err := streamAcquirer.GetStreamResult(ctx, sdHash)
    if err != nil {
        fmt.Printf("Error acquiring stream: %v\n", err)
        return
    }
    
    fmt.Printf("Stream acquired successfully. Size: %d bytes\n", result.Size())
}
```

### Creating a Stream Claim

```go
package main

import (
    "fmt"
    "go.lumeweb.com/liblbry/claim"
)

func main() {
    // Create a claim for a video stream
    h, err := claim.NewStream("Title", "Description", sdHashHex, "video/mp4", nil)
    if err != nil {
        panic(err)
    }

    // Compile into value bytes for a claim script
    value, err := h.CompileValue()
    if err != nil {
        panic(err)
    }
    fmt.Printf("Claim value (%d bytes): %x\n", len(value), value)
}
```

### Signing a Stream with a Channel

```go
package main

import (
    "fmt"
    "go.lumeweb.com/liblbry/claim"
    "github.com/lbryio/lbcd/btcec"
)

func main() {
    channelPrivKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), scalar)
    channelID := []byte{...} // 20-byte claim ID

    h, _ := claim.NewStream("Signed Video", "", sdHashHex, "video/mp4", channelID)

    // firstInputTxID is the txid in display byte order (RPC format)
    err := claim.SignStream(h, channelPrivKey, firstInputTxID, channelID)
    if err != nil {
        panic(err)
    }

    signedValue, _ := h.CompileValue()
    fmt.Printf("Signed claim value (%d bytes)\n", len(signedValue))
}
```

### Wallet and Address Derivation

```go
package main

import (
    "fmt"
    "go.lumeweb.com/liblbry/wallet"
)

func main() {
    // Create wallet from mnemonic
    w, err := wallet.NewFromMnemonic(wallet.TestMnemonic)
    if err != nil {
        panic(err)
    }

    // Derive the first 5 receiving addresses (LBRY path m/0/N)
    for i := 0; i < 5; i++ {
        addr, err := w.AddressStringAt(0, uint32(i))
        if err != nil {
            panic(err)
        }
        fmt.Printf("Address %d: %s\n", i, addr)
    }
}
```

### Coin Selection

```go
package main

import (
    "fmt"
    "go.lumeweb.com/liblbry/coinselect"
)

func main() {
    feePerByte := int64(1)

    utxos := []coinselect.UTXO{
        {TxID: "abc...", Vout: 0, Amount: 50000},
        {TxID: "def...", Vout: 1, Amount: 30000},
    }
    // Populate Fee and EffectiveAmount for each UTXO at the given fee rate
    utxos = coinselect.Estimate(utxos, feePerByte)

    target := int64(40000)
    costOfChange := int64(1000) // extra output fee

    // BnB → closest-match → accumulator fallback
    s := coinselect.NewSelector(target, costOfChange)
    result := s.Standard(utxos)

    fmt.Printf("Selected %d UTXOs, total=%d effective=%d waste=%d exact=%v tries=%d\n",
        len(result.UTXOs), result.Total, result.Effective, result.Waste, result.ExactMatch, result.Tries)
}
```

### Blob Transfer

```go
package main

import (
    "context"
    "fmt"
    "go.lumeweb.com/liblbry/blob/transfer/peer_transfer"
    "go.lumeweb.com/liblbry/protocol"
    "go.lumeweb.com/liblbry/storage/memory"
    "go.uber.org/zap"
)

func main() {
    // Create a memory storage backend
    storage := memory.NewMemoryStore()
    
    // Create a DHT node for peer discovery
    dhtNode, err := protocol.NewDHTNodeWithDefaults()
    if err != nil {
        panic(fmt.Sprintf("Failed to create DHT node: %v", err))
    }
    
    // Create a peer transfer with proper DHT node and peer client factory
    // Note: peerClientFactory can be nil for basic usage, or you can provide a custom factory
    transfer, err := peer_transfer.NewPeerTransfer(dhtNode, nil) // DHT node and client factory required
    if err != nil {
        panic(fmt.Sprintf("Failed to create peer transfer: %v", err))
    }
    
    // Example of getting a blob (requires valid hash)
    ctx := context.Background()
    hash := "sample_blob_hash"
    
    data, err := transfer.Get(ctx, hash)
    if err != nil {
        fmt.Printf("Error fetching blob: %v\n", err)
        return
    }
    
    fmt.Printf("Blob fetched successfully. Size: %d bytes\n", len(data))
}
```

### Storage Operations

```go
package main

import (
    "context"
    "fmt"
    "go.lumeweb.com/liblbry/storage/memory"
    "go.uber.org/zap"
)

func main() {
    // Create a memory storage backend
    storage := memory.NewMemoryStore()
    
    // Create some test data
    testData := []byte("Hello, liblbry!")
    hash := "sample_hash"
    
    // Put data into storage
    ctx := context.Background()
    err := storage.Put(ctx, hash, testData)
    if err != nil {
        fmt.Printf("Error putting data: %v\n", err)
        return
    }
    
    // Get data from storage
    data, err := storage.Get(ctx, hash)
    if err != nil {
        fmt.Printf("Error getting data: %v\n", err)
        return
    }
    
    fmt.Printf("Data retrieved: %s\n", string(data))
}
```

## Production Considerations

### Logger Configuration
The examples in this documentation use `zap.NewDevelopment()` for clarity and debugging. For production deployments, use `zap.NewProduction()` instead:

```go
// Production logger configuration
logger, err := zap.NewProduction()
if err != nil {
    panic(fmt.Sprintf("Failed to create production logger: %v", err))
}
```

Development logger provides verbose, human-readable output suitable for examples and debugging, while production logger provides structured, optimized logging with appropriate log levels for production environments.

## Contributing

Contributions are welcome! Please open an issue before submitting pull requests if you are planning on large changes.

## License

This project is licensed under the MIT License - see the LICENSE file for details.