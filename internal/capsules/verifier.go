package capsules

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// Verifier verifies capsule ZIPs.
type Verifier struct{}

// NewVerifier creates a new capsule verifier.
func NewVerifier() *Verifier {
	return &Verifier{}
}

// Verify verifies a capsule ZIP file.
func (v *Verifier) Verify(zipReader *zip.Reader) (*contracts.CapsuleVerifyResult, error) {
	result := &contracts.CapsuleVerifyResult{
		Valid:  false,
		Errors: []string{},
	}

	// Build file map
	fileMap := make(map[string]*zip.File)
	for _, f := range zipReader.File {
		fileMap[f.Name] = f
	}

	// Read capsule.json
	capsuleFile, ok := fileMap["capsule.json"]
	if !ok {
		result.Errors = append(result.Errors, "missing capsule.json")
		return result, nil
	}

	capsuleJSON, err := readZipFile(capsuleFile)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to read capsule.json: %v", err))
		return result, nil
	}

	var capsule contracts.CapsuleDescriptor
	if err := json.Unmarshal(capsuleJSON, &capsule); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to parse capsule.json: %v", err))
		return result, nil
	}

	result.CapsuleID = capsule.CapsuleID
	result.RunID = capsule.RunID
	result.PublicKeyFingerprint = capsule.PublicKeyFingerprint

	// Read manifest.json
	manifestFile, ok := fileMap["manifest.json"]
	if !ok {
		result.Errors = append(result.Errors, "missing manifest.json")
		return result, nil
	}

	manifestJSON, err := readZipFile(manifestFile)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to read manifest.json: %v", err))
		return result, nil
	}

	var manifest contracts.CapsuleManifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to parse manifest.json: %v", err))
		return result, nil
	}

	// Verify manifest SHA256
	manifestSHA256 := sha256Hex(manifestJSON)
	result.ManifestSHA256 = manifestSHA256

	if capsule.ManifestSHA256 != manifestSHA256 {
		result.Errors = append(result.Errors, fmt.Sprintf("manifest SHA256 mismatch: expected %s, got %s", capsule.ManifestSHA256, manifestSHA256))
		return result, nil
	}

	// Verify run_id matches
	if capsule.RunID != manifest.RunID {
		result.Errors = append(result.Errors, fmt.Sprintf("run_id mismatch between capsule and manifest"))
		return result, nil
	}

	// Read public key
	pubKeyFile, ok := fileMap["signatures/public_key.pem"]
	if !ok {
		result.Errors = append(result.Errors, "missing signatures/public_key.pem")
		return result, nil
	}

	pubKeyPEM, err := readZipFile(pubKeyFile)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to read public_key.pem: %v", err))
		return result, nil
	}

	pubKey, err := base64.StdEncoding.DecodeString(string(pubKeyPEM))
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("invalid public key encoding: %v", err))
		return result, nil
	}

	// Verify public key fingerprint
	computedFingerprint := ComputeFingerprint(pubKey)
	if capsule.PublicKeyFingerprint != computedFingerprint {
		result.Errors = append(result.Errors, fmt.Sprintf("public key fingerprint mismatch"))
		return result, nil
	}

	// Read signatures
	manifestSigFile, ok := fileMap["signatures/manifest.sig"]
	if !ok {
		result.Errors = append(result.Errors, "missing signatures/manifest.sig")
		return result, nil
	}

	manifestSig, err := readZipFile(manifestSigFile)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to read manifest.sig: %v", err))
		return result, nil
	}

	capsuleSigFile, ok := fileMap["signatures/capsule.sig"]
	if !ok {
		result.Errors = append(result.Errors, "missing signatures/capsule.sig")
		return result, nil
	}

	capsuleSig, err := readZipFile(capsuleSigFile)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to read capsule.sig: %v", err))
		return result, nil
	}

	// Verify manifest signature
	if !VerifyWithKey(pubKey, manifestJSON, manifestSig) {
		result.Errors = append(result.Errors, "manifest signature verification failed")
		return result, nil
	}

	// Verify capsule signature (over capsule.json + manifest SHA256)
	capsuleSigData := append(capsuleJSON, []byte(manifestSHA256)...)
	if !VerifyWithKey(pubKey, capsuleSigData, capsuleSig) {
		result.Errors = append(result.Errors, "capsule signature verification failed")
		return result, nil
	}

	// Verify all files in manifest
	filesVerified := 0
	for _, mf := range manifest.Files {
		// Check for path traversal
		if strings.Contains(mf.Path, "..") {
			result.Errors = append(result.Errors, fmt.Sprintf("path traversal detected: %s", mf.Path))
			return result, nil
		}

		// Normalize path
		normalizedPath := filepath.ToSlash(mf.Path)

		f, ok := fileMap[normalizedPath]
		if !ok {
			result.Errors = append(result.Errors, fmt.Sprintf("file not found in archive: %s", mf.Path))
			return result, nil
		}

		// Verify hash
		data, err := readZipFile(f)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to read file %s: %v", mf.Path, err))
			return result, nil
		}

		hash := sha256.Sum256(data)
		computedHash := hex.EncodeToString(hash[:])

		if mf.SHA256 != computedHash {
			result.Errors = append(result.Errors, fmt.Sprintf("hash mismatch for %s: expected %s, got %s", mf.Path, mf.SHA256, computedHash))
			return result, nil
		}

		// Verify size
		if mf.SizeBytes != int64(len(data)) {
			result.Errors = append(result.Errors, fmt.Sprintf("size mismatch for %s: expected %d, got %d", mf.Path, mf.SizeBytes, len(data)))
			return result, nil
		}

		filesVerified++
	}

	// Verify run.json exists and run_id matches
	runFile, ok := fileMap[fmt.Sprintf("runs/%s/run.json", manifest.RunID)]
	if !ok {
		result.Errors = append(result.Errors, "run.json not found at expected path")
		return result, nil
	}

	runJSON, err := readZipFile(runFile)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to read run.json: %v", err))
		return result, nil
	}

	var run contracts.WorkflowRun
	if err := json.Unmarshal(runJSON, &run); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to parse run.json: %v", err))
		return result, nil
	}

	if run.ID != manifest.RunID {
		result.Errors = append(result.Errors, fmt.Sprintf("run.json run_id mismatch: expected %s, got %s", manifest.RunID, run.ID))
		return result, nil
	}

	result.FilesVerified = filesVerified
	result.Valid = true
	return result, nil
}

// VerifyFile verifies a capsule ZIP file from disk.
func (v *Verifier) VerifyFile(path string) (*contracts.CapsuleVerifyResult, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	return v.Verify(&r.Reader)
}

// readZipFile reads the contents of a zip file entry.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return io.ReadAll(rc)
}
