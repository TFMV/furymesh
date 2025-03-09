# Changelog

All notable changes to the FuryMesh project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **End-to-End Encryption**: Implemented robust end-to-end encryption for secure file transfers
  - RSA-based key exchange for secure session key distribution
  - AES-256-GCM encryption for file chunks
  - Secure key storage with proper permissions
  - Automatic session key management
  - Transparent encryption/decryption during file transfers

- **DHT-based Peer Discovery**: Implemented Kademlia distributed hash table for decentralized peer discovery
  - Efficient node lookup with O(log n) complexity
  - Automatic routing table maintenance
  - Persistent storage of file metadata in the DHT
  - Bootstrap node support for initial network joining
  - Periodic announcements of available files
  - Configurable refresh and republish intervals

### Changed

- Updated configuration file with new settings for encryption and DHT
- Enhanced file transfer system to support encrypted transfers
- Improved node initialization to include DHT bootstrapping
- Updated README with information about new security features

### Fixed

- Various linter errors in the codebase
- Improved error handling in file transfer code

## [0.1.0] - 2023-06-01

### Added

- Initial release of FuryMesh
- WebRTC-based P2P communication
- Basic file chunking and transfer
- CLI interface for node management
- Monitoring API for system status
