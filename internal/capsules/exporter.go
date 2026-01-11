package capsules

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
	"github.com/AIfactory-hq/qfactory/pkg/events"
)

// Exporter builds and signs capsule exports.
type Exporter struct {
	signer      *Signer
	evidenceDir string
}

// NewExporter creates a new capsule exporter.
func NewExporter(signer *Signer, evidenceDir string) *Exporter {
	return &Exporter{
		signer:      signer,
		evidenceDir: evidenceDir,
	}
}

// ExportParams contains parameters for exporting a capsule.
type ExportParams struct {
	Run         *contracts.WorkflowRun
	Events      []events.Event
	Trust       contracts.TrustIndex
	GateHistory []contracts.GateHistoryItem
	FinalizedAt time.Time
	FinalizedBy string
	Override    bool
	OverrideReason string
}

// ExportResult contains the result of a capsule export.
type ExportResult struct {
	CapsuleID      string
	CapsulePath    string
	ManifestSHA256 string
}

// Export creates a signed capsule ZIP for the given run.
func (e *Exporter) Export(params ExportParams) (*ExportResult, error) {
	run := params.Run
	if run == nil {
		return nil, fmt.Errorf("run is nil")
	}

	// Generate capsule ID
	capsuleID := events.NewID()

	// Create capsule directory
	capsuleDir := filepath.Join(e.evidenceDir, run.ID, "capsule", capsuleID)
	if err := os.MkdirAll(capsuleDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create capsule directory: %w", err)
	}

	// Create staging directory for capsule contents
	stagingDir := filepath.Join(capsuleDir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create staging directory: %w", err)
	}

	// Create runs subdirectory
	runsDir := filepath.Join(stagingDir, "runs", run.ID)
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create runs directory: %w", err)
	}

	// Create signatures directory
	sigsDir := filepath.Join(stagingDir, "signatures")
	if err := os.MkdirAll(sigsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create signatures directory: %w", err)
	}

	// Write run.json
	runJSON, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runsDir, "run.json"), runJSON, 0644); err != nil {
		return nil, fmt.Errorf("failed to write run.json: %w", err)
	}

	// Write events.jsonl
	eventsPath := filepath.Join(runsDir, "events.jsonl")
	eventsFile, err := os.Create(eventsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create events.jsonl: %w", err)
	}
	for _, evt := range params.Events {
		line, err := json.Marshal(evt)
		if err != nil {
			eventsFile.Close()
			return nil, fmt.Errorf("failed to marshal event: %w", err)
		}
		eventsFile.Write(line)
		eventsFile.Write([]byte("\n"))
	}
	eventsFile.Close()

	// Write trust.json
	trustJSON, err := json.MarshalIndent(params.Trust, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal trust: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runsDir, "trust.json"), trustJSON, 0644); err != nil {
		return nil, fmt.Errorf("failed to write trust.json: %w", err)
	}

	// Write gate_history.json
	historyJSON, err := json.MarshalIndent(params.GateHistory, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gate history: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runsDir, "gate_history.json"), historyJSON, 0644); err != nil {
		return nil, fmt.Errorf("failed to write gate_history.json: %w", err)
	}

	// Copy evidence directory
	srcEvidenceDir := filepath.Join(e.evidenceDir, run.ID)
	dstEvidenceDir := filepath.Join(runsDir, "evidence")
	if _, err := os.Stat(srcEvidenceDir); err == nil {
		if err := copyDir(srcEvidenceDir, dstEvidenceDir); err != nil {
			return nil, fmt.Errorf("failed to copy evidence: %w", err)
		}
	}

	// Build file manifest
	var files []contracts.CapsuleFile
	var totalSize int64
	err = filepath.Walk(stagingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(stagingDir, path)
		if err != nil {
			return err
		}

		// Compute SHA256
		hash, err := computeFileSHA256(path)
		if err != nil {
			return err
		}

		files = append(files, contracts.CapsuleFile{
			Path:      relPath,
			SHA256:    hash,
			SizeBytes: info.Size(),
		})
		totalSize += info.Size()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk staging directory: %w", err)
	}

	// Sort files for deterministic output
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	// Create manifest
	manifest := contracts.CapsuleManifest{
		CapsuleID:   capsuleID,
		RunID:       run.ID,
		GeneratedAt: params.FinalizedAt,
		Files:       files,
		TotalSize:   totalSize,
		FileCount:   len(files),
	}

	// Write manifest.json
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(stagingDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestJSON, 0644); err != nil {
		return nil, fmt.Errorf("failed to write manifest.json: %w", err)
	}

	// Compute manifest SHA256
	manifestSHA256 := sha256Hex(manifestJSON)

	// Create capsule descriptor
	capsule := contracts.CapsuleDescriptor{
		CapsuleID:            capsuleID,
		RunID:                run.ID,
		Version:              "1.0",
		GeneratedAt:          params.FinalizedAt,
		ManifestSHA256:       manifestSHA256,
		TrustScore:           params.Trust.Score,
		TrustGrade:           params.Trust.Grade,
		FinalizedAt:          params.FinalizedAt,
		FinalizedBy:          params.FinalizedBy,
		Override:             params.Override,
		OverrideReason:       params.OverrideReason,
		PublicKeyFingerprint: e.signer.PublicKeyFingerprint(),
	}

	// Write capsule.json
	capsuleJSON, err := json.MarshalIndent(capsule, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal capsule: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "capsule.json"), capsuleJSON, 0644); err != nil {
		return nil, fmt.Errorf("failed to write capsule.json: %w", err)
	}

	// Sign manifest
	manifestSig := e.signer.Sign(manifestJSON)
	if err := os.WriteFile(filepath.Join(sigsDir, "manifest.sig"), manifestSig, 0644); err != nil {
		return nil, fmt.Errorf("failed to write manifest.sig: %w", err)
	}

	// Sign capsule (over capsule.json + manifest SHA256)
	capsuleSigData := append(capsuleJSON, []byte(manifestSHA256)...)
	capsuleSig := e.signer.Sign(capsuleSigData)
	if err := os.WriteFile(filepath.Join(sigsDir, "capsule.sig"), capsuleSig, 0644); err != nil {
		return nil, fmt.Errorf("failed to write capsule.sig: %w", err)
	}

	// Write public key
	if err := os.WriteFile(filepath.Join(sigsDir, "public_key.pem"), []byte(e.signer.PublicKeyPEM()), 0644); err != nil {
		return nil, fmt.Errorf("failed to write public_key.pem: %w", err)
	}

	// Create ZIP file
	zipPath := filepath.Join(capsuleDir, "capsule.zip")
	if err := createZip(stagingDir, zipPath); err != nil {
		return nil, fmt.Errorf("failed to create zip: %w", err)
	}

	// Clean up staging directory
	os.RemoveAll(stagingDir)

	return &ExportResult{
		CapsuleID:      capsuleID,
		CapsulePath:    zipPath,
		ManifestSHA256: manifestSHA256,
	}, nil
}

// copyDir recursively copies a directory, excluding the "capsule" subdirectory.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip capsule directory to avoid recursion
		if relPath == "capsule" || (len(relPath) > 8 && relPath[:8] == "capsule"+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Copy file
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

// computeFileSHA256 computes the SHA256 hash of a file.
func computeFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// sha256Hex computes SHA256 of data and returns hex string.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// createZip creates a ZIP file from a directory with sorted entries.
func createZip(srcDir, zipPath string) error {
	// Collect all files
	var files []string
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(srcDir, path)
			if err != nil {
				return err
			}
			files = append(files, relPath)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Sort for deterministic output
	sort.Strings(files)

	// Create ZIP
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	w := zip.NewWriter(zipFile)
	defer w.Close()

	for _, relPath := range files {
		srcPath := filepath.Join(srcDir, relPath)

		// Open source file
		srcFile, err := os.Open(srcPath)
		if err != nil {
			return err
		}

		// Get file info
		info, err := srcFile.Stat()
		if err != nil {
			srcFile.Close()
			return err
		}

		// Create ZIP header with fixed timestamp for reproducibility
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			srcFile.Close()
			return err
		}
		header.Name = filepath.ToSlash(relPath)
		header.Method = zip.Deflate
		// Use a fixed timestamp for reproducibility
		header.Modified = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		dst, err := w.CreateHeader(header)
		if err != nil {
			srcFile.Close()
			return err
		}

		_, err = io.Copy(dst, srcFile)
		srcFile.Close()
		if err != nil {
			return err
		}
	}

	return nil
}
