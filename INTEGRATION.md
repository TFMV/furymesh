# FuryMesh File Transfer Integration

This document outlines the integration of the file transfer system with the FuryMesh node.

## Components

The file transfer system consists of the following components:

1. **Chunker (`file/chunker.go`)**: Handles splitting files into chunks and reassembling them.
2. **Storage Manager (`file/storage.go`)**: Manages persistent storage of files and metadata.
3. **Transfer Manager (`file/transfer.go`)**: Coordinates file transfers between peers.
4. **WebRTC Transport (`file/webrtc.go`)**: Handles WebRTC connections for file transfers.
5. **File Manager (`node/file_integration.go`)**: Integrates the file transfer system with the node.
6. **CLI Commands (`cmd/file.go` and `cmd/file_node.go`)**: Provides command-line interface for file operations.

## Integration Flow

The integration follows this flow:

1. When a node starts, it initializes a `FileManager` which in turn initializes the chunker, storage manager, transfer manager, and WebRTC transport.
2. The `FileManager` is attached to the node, allowing it to handle file operations.
3. When peers connect, they automatically share information about available files.
4. Files can be transferred directly between peers using WebRTC data channels.
5. The CLI commands provide a user interface for interacting with the file transfer system.

## Configuration

The file transfer system is configured through the `config.yaml` file, which includes settings for:

- Storage locations and chunk size
- Transfer timeouts and retries
- WebRTC configuration for NAT traversal

## Usage

### Node Commands

```bash
# Start a node
furymesh node
```

### File Commands

```bash
# Chunk a file
furymesh file chunk /path/to/file.txt

# List available files
furymesh file list

# Reassemble a file
furymesh file reassemble <file-id> /path/to/output.txt

# Delete a file
furymesh file delete <file-id>

# Clean up expired files
furymesh file cleanup
```

### Node File Commands

```bash
# Request a file from a peer
furymesh file request --peer <peer-id> --file <file-id>

# Check transfer status
furymesh file status --file <file-id>

# List peers with available files
furymesh file peers
```

## Implementation Details

### File Chunking

Files are split into chunks of configurable size (default 1MB). Each chunk is hashed for integrity verification. Metadata about the file and its chunks is stored for later reassembly.

### Transfer Management

The transfer manager handles the coordination of file transfers, including:

- Requesting chunks from peers
- Retrying failed transfers
- Tracking transfer progress
- Managing concurrent transfers

### WebRTC Integration

The WebRTC transport handles the peer-to-peer communication, including:

- Establishing WebRTC connections
- Creating data channels for file transfers
- Handling data channel messages
- Managing peer connections and disconnections

### Node Integration

The node integration allows the file transfer system to:

- Automatically discover files from peers
- Share available files with peers
- Handle file requests from peers
- Manage file transfers in the background

## Future Improvements

1. **Bandwidth Control**: Implement bandwidth throttling to prevent network congestion.
2. **Encryption**: Add end-to-end encryption for file transfers.
3. **Multi-peer Transfers**: Allow downloading different chunks from different peers.
4. **Resume Support**: Add support for resuming interrupted transfers.
5. **Web Interface**: Create a web interface for managing file transfers.
6. **Mobile Support**: Extend the system to work on mobile devices.
