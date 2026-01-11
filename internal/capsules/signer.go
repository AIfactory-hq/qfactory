// Package capsules provides capsule export, signing, and verification.
package capsules

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
)

// Signer handles Ed25519 signing and verification.
type Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	mu         sync.RWMutex
}

// NewSigner creates a new signer, loading keys from environment.
// Priority:
// 1. QFACTORY_SIGNING_PRIVATE_KEY + QFACTORY_SIGNING_PUBLIC_KEY (base64)
// 2. QFACTORY_DEV_SIGNING_SEED (base64) - derive keypair from seed
// 3. Generate random keypair (dev mode, logs warning)
func NewSigner() (*Signer, error) {
	s := &Signer{}

	// Try loading from explicit keys
	privKeyB64 := os.Getenv("QFACTORY_SIGNING_PRIVATE_KEY")
	pubKeyB64 := os.Getenv("QFACTORY_SIGNING_PUBLIC_KEY")

	if privKeyB64 != "" && pubKeyB64 != "" {
		privKey, err := base64.StdEncoding.DecodeString(privKeyB64)
		if err != nil {
			return nil, fmt.Errorf("invalid QFACTORY_SIGNING_PRIVATE_KEY: %w", err)
		}
		pubKey, err := base64.StdEncoding.DecodeString(pubKeyB64)
		if err != nil {
			return nil, fmt.Errorf("invalid QFACTORY_SIGNING_PUBLIC_KEY: %w", err)
		}

		if len(privKey) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("invalid private key size: expected %d, got %d", ed25519.PrivateKeySize, len(privKey))
		}
		if len(pubKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid public key size: expected %d, got %d", ed25519.PublicKeySize, len(pubKey))
		}

		s.privateKey = ed25519.PrivateKey(privKey)
		s.publicKey = ed25519.PublicKey(pubKey)
		return s, nil
	}

	// Try deriving from seed
	seedB64 := os.Getenv("QFACTORY_DEV_SIGNING_SEED")
	if seedB64 != "" {
		seed, err := base64.StdEncoding.DecodeString(seedB64)
		if err != nil {
			return nil, fmt.Errorf("invalid QFACTORY_DEV_SIGNING_SEED: %w", err)
		}
		if len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("invalid seed size: expected %d, got %d", ed25519.SeedSize, len(seed))
		}

		s.privateKey = ed25519.NewKeyFromSeed(seed)
		s.publicKey = s.privateKey.Public().(ed25519.PublicKey)
		return s, nil
	}

	// Generate random keypair (dev mode)
	fmt.Println("WARNING: Generating random signing keypair (dev mode only)")
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate keypair: %w", err)
	}

	s.privateKey = privKey
	s.publicKey = pubKey
	return s, nil
}

// Sign signs the given data and returns the signature.
func (s *Signer) Sign(data []byte) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ed25519.Sign(s.privateKey, data)
}

// Verify verifies the signature over the given data.
func (s *Signer) Verify(data, signature []byte) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ed25519.Verify(s.publicKey, data, signature)
}

// VerifyWithKey verifies a signature using a specific public key.
func VerifyWithKey(publicKey, data, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(publicKey), data, signature)
}

// PublicKey returns the public key bytes.
func (s *Signer) PublicKey() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return []byte(s.publicKey)
}

// PublicKeyPEM returns the public key in PEM-like format.
// (Simple base64 for now, not full PEM)
func (s *Signer) PublicKeyPEM() string {
	return base64.StdEncoding.EncodeToString(s.PublicKey())
}

// PublicKeyFingerprint returns a hex-encoded SHA256 fingerprint of the public key.
func (s *Signer) PublicKeyFingerprint() string {
	hash := sha256.Sum256(s.PublicKey())
	return hex.EncodeToString(hash[:])
}

// ComputeFingerprint computes SHA256 fingerprint of a public key.
func ComputeFingerprint(publicKey []byte) string {
	hash := sha256.Sum256(publicKey)
	return hex.EncodeToString(hash[:])
}
