package crypto

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestEncryptionManager(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "encryption-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create an encryption manager
	em, err := NewEncryptionManager(logger, tempDir)
	if err != nil {
		t.Fatalf("Failed to create encryption manager: %v", err)
	}

	// Test key generation
	t.Run("KeyGeneration", func(t *testing.T) {
		// Check if keys were generated
		if em.privateKey == nil {
			t.Error("Private key was not generated")
		}
		if em.publicKey == nil {
			t.Error("Public key was not generated")
		}

		// Check if key files were created
		privateKeyPath := filepath.Join(tempDir, "private.pem")
		publicKeyPath := filepath.Join(tempDir, "public.pem")

		if _, err := os.Stat(privateKeyPath); os.IsNotExist(err) {
			t.Error("Private key file was not created")
		}
		if _, err := os.Stat(publicKeyPath); os.IsNotExist(err) {
			t.Error("Public key file was not created")
		}
	})

	// Test session key generation
	t.Run("SessionKeyGeneration", func(t *testing.T) {
		sessionKey, err := em.GenerateSessionKey()
		if err != nil {
			t.Fatalf("Failed to generate session key: %v", err)
		}
		if len(sessionKey) != KeySize {
			t.Errorf("Session key has wrong size: got %d, want %d", len(sessionKey), KeySize)
		}
	})

	// Test data encryption and decryption
	t.Run("DataEncryptionDecryption", func(t *testing.T) {
		// Generate a session key
		sessionKey, err := em.GenerateSessionKey()
		if err != nil {
			t.Fatalf("Failed to generate session key: %v", err)
		}

		// Test data
		originalData := []byte("This is a test message for encryption and decryption")

		// Encrypt the data
		encryptedData, err := em.EncryptData(originalData, sessionKey)
		if err != nil {
			t.Fatalf("Failed to encrypt data: %v", err)
		}

		// Verify that the encrypted data is different from the original
		if string(encryptedData) == string(originalData) {
			t.Error("Encrypted data is the same as original data")
		}

		// Decrypt the data
		decryptedData, err := em.DecryptData(encryptedData, sessionKey)
		if err != nil {
			t.Fatalf("Failed to decrypt data: %v", err)
		}

		// Verify that the decrypted data matches the original
		if string(decryptedData) != string(originalData) {
			t.Errorf("Decrypted data does not match original: got %s, want %s",
				string(decryptedData), string(originalData))
		}
	})

	// Test chunk encryption and decryption
	t.Run("ChunkEncryptionDecryption", func(t *testing.T) {
		// Generate a session key
		sessionKey, err := em.GenerateSessionKey()
		if err != nil {
			t.Fatalf("Failed to generate session key: %v", err)
		}

		// Test chunk data
		originalChunk := []byte("This is a test chunk for encryption and decryption")

		// Encrypt the chunk
		encryptedChunk, err := em.EncryptChunk(originalChunk, sessionKey)
		if err != nil {
			t.Fatalf("Failed to encrypt chunk: %v", err)
		}

		// Decrypt the chunk
		decryptedChunk, err := em.DecryptChunk(encryptedChunk, sessionKey)
		if err != nil {
			t.Fatalf("Failed to decrypt chunk: %v", err)
		}

		// Verify that the decrypted chunk matches the original
		if string(decryptedChunk) != string(originalChunk) {
			t.Errorf("Decrypted chunk does not match original: got %s, want %s",
				string(decryptedChunk), string(originalChunk))
		}
	})

	// Test peer key management
	t.Run("PeerKeyManagement", func(t *testing.T) {
		// Get public key PEM
		publicKeyPEM, err := em.GetPublicKeyPEM()
		if err != nil {
			t.Fatalf("Failed to get public key PEM: %v", err)
		}

		// Add peer public key
		err = em.AddPeerPublicKey("test-peer", publicKeyPEM)
		if err != nil {
			t.Fatalf("Failed to add peer public key: %v", err)
		}

		// Generate a session key
		sessionKey, err := em.GenerateSessionKey()
		if err != nil {
			t.Fatalf("Failed to generate session key: %v", err)
		}

		// Encrypt session key for peer
		encryptedKey, err := em.EncryptSessionKey("test-peer", sessionKey)
		if err != nil {
			t.Fatalf("Failed to encrypt session key: %v", err)
		}

		// Decrypt session key
		decryptedKey, err := em.DecryptSessionKey(encryptedKey)
		if err != nil {
			t.Fatalf("Failed to decrypt session key: %v", err)
		}

		// Verify that the decrypted key matches the original
		if string(decryptedKey) != string(sessionKey) {
			t.Error("Decrypted session key does not match original")
		}
	})
}
