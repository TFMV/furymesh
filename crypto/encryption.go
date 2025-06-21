package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"

	"github.com/TFMV/furymesh/metrics"
)

const (
	// KeySize is the size of the AES key in bytes (256 bits)
	KeySize = 32
	// NonceSize is the size of the nonce in bytes
	NonceSize = 12
	// RSAKeySize is the size of the RSA key in bits
	RSAKeySize = 2048
)

// EncryptionManager handles encryption and decryption of data
type EncryptionManager struct {
	logger     *zap.Logger
	keysDir    string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	peerKeys   map[string]*rsa.PublicKey
	mu         sync.RWMutex
}

// NewEncryptionManager creates a new EncryptionManager
func NewEncryptionManager(logger *zap.Logger, keysDir string) (*EncryptionManager, error) {
	// Create keys directory if it doesn't exist
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create keys directory: %w", err)
	}

	em := &EncryptionManager{
		logger:   logger,
		keysDir:  keysDir,
		peerKeys: make(map[string]*rsa.PublicKey),
	}

	// Load or generate keys
	if err := em.loadOrGenerateKeys(); err != nil {
		return nil, fmt.Errorf("failed to load or generate keys: %w", err)
	}

	return em, nil
}

// loadOrGenerateKeys loads existing keys or generates new ones if they don't exist
func (em *EncryptionManager) loadOrGenerateKeys() error {
	privateKeyPath := filepath.Join(em.keysDir, "private.pem")
	publicKeyPath := filepath.Join(em.keysDir, "public.pem")

	// Check if keys exist
	if _, err := os.Stat(privateKeyPath); os.IsNotExist(err) {
		// Generate new keys
		em.logger.Info("Generating new RSA key pair")
		if err := em.generateKeys(privateKeyPath, publicKeyPath); err != nil {
			return fmt.Errorf("failed to generate keys: %w", err)
		}
	} else {
		// Load existing keys
		em.logger.Info("Loading existing RSA key pair")
		if err := em.loadKeys(privateKeyPath, publicKeyPath); err != nil {
			return fmt.Errorf("failed to load keys: %w", err)
		}
	}

	return nil
}

// generateKeys generates a new RSA key pair and saves them to disk
func (em *EncryptionManager) generateKeys(privateKeyPath, publicKeyPath string) error {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, RSAKeySize)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Save private key
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	if err := os.WriteFile(privateKeyPath, privateKeyPEM, 0600); err != nil {
		return fmt.Errorf("failed to save private key: %w", err)
	}

	// Save public key
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	if err := os.WriteFile(publicKeyPath, publicKeyPEM, 0644); err != nil {
		return fmt.Errorf("failed to save public key: %w", err)
	}

	em.privateKey = privateKey
	em.publicKey = &privateKey.PublicKey

	return nil
}

// loadKeys loads RSA keys from disk
func (em *EncryptionManager) loadKeys(privateKeyPath, publicKeyPath string) error {
	// Load private key
	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read private key: %w", err)
	}

	privateKeyBlock, _ := pem.Decode(privateKeyPEM)
	if privateKeyBlock == nil {
		return errors.New("failed to parse private key PEM")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(privateKeyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Load public key
	publicKeyPEM, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key: %w", err)
	}

	publicKeyBlock, _ := pem.Decode(publicKeyPEM)
	if publicKeyBlock == nil {
		return errors.New("failed to parse public key PEM")
	}

	publicKeyInterface, err := x509.ParsePKIXPublicKey(publicKeyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse public key: %w", err)
	}

	publicKey, ok := publicKeyInterface.(*rsa.PublicKey)
	if !ok {
		return errors.New("not an RSA public key")
	}

	em.privateKey = privateKey
	em.publicKey = publicKey

	return nil
}

// GetPublicKeyPEM returns the PEM-encoded public key
func (em *EncryptionManager) GetPublicKeyPEM() ([]byte, error) {
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(em.publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	return publicKeyPEM, nil
}

// AddPeerPublicKey adds a peer's public key
func (em *EncryptionManager) AddPeerPublicKey(peerID string, publicKeyPEM []byte) error {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return errors.New("failed to parse peer public key PEM")
	}

	publicKeyInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse peer public key: %w", err)
	}

	publicKey, ok := publicKeyInterface.(*rsa.PublicKey)
	if !ok {
		return errors.New("not an RSA public key")
	}

	em.mu.Lock()
	em.peerKeys[peerID] = publicKey
	em.mu.Unlock()

	em.logger.Info("Added peer public key", zap.String("peer_id", peerID))
	return nil
}

// GenerateSessionKey generates a random AES key for a session
func (em *EncryptionManager) GenerateSessionKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate session key: %w", err)
	}
	return key, nil
}

// EncryptSessionKey encrypts a session key with a peer's public key
func (em *EncryptionManager) EncryptSessionKey(peerID string, sessionKey []byte) ([]byte, error) {
	em.mu.RLock()
	publicKey, exists := em.peerKeys[peerID]
	em.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("public key for peer %s not found", peerID)
	}

	encryptedKey, err := rsa.EncryptOAEP(
		sha256.New(),
		rand.Reader,
		publicKey,
		sessionKey,
		nil,
	)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return nil, fmt.Errorf("failed to encrypt session key: %w", err)
	}

	return encryptedKey, nil
}

// DecryptSessionKey decrypts a session key with our private key
func (em *EncryptionManager) DecryptSessionKey(encryptedKey []byte) ([]byte, error) {
	sessionKey, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		em.privateKey,
		encryptedKey,
		nil,
	)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return nil, fmt.Errorf("failed to decrypt session key: %w", err)
	}

	return sessionKey, nil
}

// EncryptData encrypts data with a session key
func (em *EncryptionManager) EncryptData(data, sessionKey []byte) ([]byte, error) {
	// Create a new AES cipher block
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Create a GCM cipher mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate a random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		metrics.EncryptionFailures.Inc()
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt the data
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

// DecryptData decrypts data with a session key
func (em *EncryptionManager) DecryptData(encryptedData, sessionKey []byte) ([]byte, error) {
	// Create a new AES cipher block
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Create a GCM cipher mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract the nonce
	nonceSize := gcm.NonceSize()
	if len(encryptedData) < nonceSize {
		metrics.EncryptionFailures.Inc()
		return nil, errors.New("encrypted data too short")
	}

	nonce, ciphertext := encryptedData[:nonceSize], encryptedData[nonceSize:]

	// Decrypt the data
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return nil, fmt.Errorf("failed to decrypt data: %w", err)
	}

	return plaintext, nil
}

// EncryptFile encrypts a file with a session key
func (em *EncryptionManager) EncryptFile(inputPath, outputPath string, sessionKey []byte) error {
	// Read the input file
	data, err := os.ReadFile(inputPath)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Encrypt the data
	encryptedData, err := em.EncryptData(data, sessionKey)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return fmt.Errorf("failed to encrypt data: %w", err)
	}

	// Write the encrypted data to the output file
	if err := os.WriteFile(outputPath, encryptedData, 0644); err != nil {
		metrics.EncryptionFailures.Inc()
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

// DecryptFile decrypts a file with a session key
func (em *EncryptionManager) DecryptFile(inputPath, outputPath string, sessionKey []byte) error {
	// Read the input file
	encryptedData, err := os.ReadFile(inputPath)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Decrypt the data
	data, err := em.DecryptData(encryptedData, sessionKey)
	if err != nil {
		metrics.EncryptionFailures.Inc()
		return fmt.Errorf("failed to decrypt data: %w", err)
	}

	// Write the decrypted data to the output file
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		metrics.EncryptionFailures.Inc()
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

// EncryptChunk encrypts a chunk with a session key
func (em *EncryptionManager) EncryptChunk(chunk []byte, sessionKey []byte) ([]byte, error) {
	return em.EncryptData(chunk, sessionKey)
}

// DecryptChunk decrypts a chunk with a session key
func (em *EncryptionManager) DecryptChunk(encryptedChunk []byte, sessionKey []byte) ([]byte, error) {
	return em.DecryptData(encryptedChunk, sessionKey)
}
