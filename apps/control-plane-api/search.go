// Package main implements repository search with Qdrant vector database.
// v0.3b uses a deterministic embedding stub that can be replaced with real embeddings later.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultEmbeddingSize = 128
	DefaultChunkSize     = 900
	DefaultChunkOverlap  = 200
	DefaultSearchK       = 8
	QdrantCollection     = "qfactory_code"
)

// SearchService handles repository indexing and search.
type SearchService struct {
	qdrant        *QdrantClient
	embeddingSize int
	chunkSize     int
	chunkOverlap  int
	mu            sync.RWMutex
}

// NewSearchService creates a new search service.
func NewSearchService(qdrantURL string) (*SearchService, error) {
	client := &QdrantClient{
		baseURL:    qdrantURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	svc := &SearchService{
		qdrant:        client,
		embeddingSize: DefaultEmbeddingSize,
		chunkSize:     DefaultChunkSize,
		chunkOverlap:  DefaultChunkOverlap,
	}

	// Ensure collection exists
	if err := svc.ensureCollection(); err != nil {
		return nil, fmt.Errorf("failed to ensure collection: %w", err)
	}

	return svc, nil
}

// ensureCollection creates the Qdrant collection if it doesn't exist.
func (s *SearchService) ensureCollection() error {
	// Check if collection exists
	resp, err := s.qdrant.httpClient.Get(s.qdrant.baseURL + "/collections/" + QdrantCollection)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil // Collection exists
	}

	// Create collection
	createReq := map[string]interface{}{
		"vectors": map[string]interface{}{
			"size":     s.embeddingSize,
			"distance": "Cosine",
		},
	}

	body, _ := json.Marshal(createReq)
	req, _ := http.NewRequest("PUT", s.qdrant.baseURL+"/collections/"+QdrantCollection, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	createResp, err := s.qdrant.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(createResp.Body)
		return fmt.Errorf("failed to create collection: %s", string(respBody))
	}

	return nil
}

// IndexRequest represents a request to index files.
type IndexRequest struct {
	Path    string   `json:"path"`              // Root path to index
	Globs   []string `json:"globs,omitempty"`   // Glob patterns to include (default: ["**/*.go", "**/*.ts", "**/*.py", "**/*.js"])
	Exclude []string `json:"exclude,omitempty"` // Patterns to exclude
}

// IndexResponse represents the result of indexing.
type IndexResponse struct {
	FilesIndexed  int      `json:"files_indexed"`
	ChunksCreated int      `json:"chunks_created"`
	Files         []string `json:"files"`
	DurationMs    int64    `json:"duration_ms"`
	Error         string   `json:"error,omitempty"`
}

// SearchResult represents a single search result.
type SearchResult struct {
	Path       string  `json:"path"`
	Score      float32 `json:"score"`
	ChunkIndex int     `json:"chunk_index"`
	SHA256     string  `json:"sha256"`
	Snippet    string  `json:"snippet"`
	Content    string  `json:"content"`
}

// SearchResponse represents search results.
type SearchResponse struct {
	Query   string         `json:"query"`
	K       int            `json:"k"`
	Results []SearchResult `json:"results"`
}

// Index indexes files from the given path.
func (s *SearchService) Index(req IndexRequest) (*IndexResponse, error) {
	startTime := time.Now()

	// Default globs
	globs := req.Globs
	if len(globs) == 0 {
		globs = []string{"**/*.go", "**/*.ts", "**/*.tsx", "**/*.py", "**/*.js", "**/*.jsx"}
	}

	// Find matching files
	var files []string
	for _, pattern := range globs {
		matches, err := s.globFiles(req.Path, pattern, req.Exclude)
		if err != nil {
			continue
		}
		files = append(files, matches...)
	}

	// Dedupe files
	seen := make(map[string]bool)
	uniqueFiles := []string{}
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			uniqueFiles = append(uniqueFiles, f)
		}
	}

	// Index each file
	var points []QdrantPoint
	for _, filePath := range uniqueFiles {
		filePoints, err := s.indexFile(filePath, req.Path)
		if err != nil {
			continue // Skip files that can't be indexed
		}
		points = append(points, filePoints...)
	}

	// Upsert to Qdrant
	if len(points) > 0 {
		if err := s.qdrant.UpsertPoints(QdrantCollection, points); err != nil {
			return nil, fmt.Errorf("failed to upsert points: %w", err)
		}
	}

	return &IndexResponse{
		FilesIndexed:  len(uniqueFiles),
		ChunksCreated: len(points),
		Files:         uniqueFiles,
		DurationMs:    time.Since(startTime).Milliseconds(),
	}, nil
}

// globFiles finds files matching the pattern under root.
func (s *SearchService) globFiles(root, pattern string, exclude []string) ([]string, error) {
	var matches []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if info.IsDir() {
			// Skip common directories
			name := info.Name()
			if name == "node_modules" || name == ".git" || name == "vendor" || name == ".next" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(root, path)

		// Check exclusions
		for _, ex := range exclude {
			if matched, _ := filepath.Match(ex, relPath); matched {
				return nil
			}
			if matched, _ := filepath.Match(ex, filepath.Base(path)); matched {
				return nil
			}
		}

		// Check pattern match
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		if !matched {
			// Try matching against full relative path for ** patterns
			if strings.HasPrefix(pattern, "**/") {
				matched, _ = filepath.Match(pattern[3:], filepath.Base(path))
			}
		}

		if matched {
			matches = append(matches, path)
		}
		return nil
	})

	return matches, err
}

// indexFile indexes a single file into chunks.
func (s *SearchService) indexFile(filePath, rootPath string) ([]QdrantPoint, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Skip binary files
	if !isTextFile(content) {
		return nil, fmt.Errorf("binary file")
	}

	text := string(content)
	chunks := ChunkText(text, s.chunkSize, s.chunkOverlap)

	relPath, _ := filepath.Rel(rootPath, filePath)
	fileHash := sha256.Sum256(content)
	fileHashStr := hex.EncodeToString(fileHash[:])

	var points []QdrantPoint
	for i, chunk := range chunks {
		vector := DeterministicEmbed(chunk, s.embeddingSize)
		pointID := fmt.Sprintf("%s:%d", fileHashStr[:16], i)

		point := QdrantPoint{
			ID:     pointID,
			Vector: vector,
			Payload: map[string]interface{}{
				"path":        relPath,
				"chunk_index": i,
				"sha256":      fileHashStr,
				"content":     chunk,
				"snippet":     truncateSnippet(chunk, 200),
			},
		}
		points = append(points, point)
	}

	return points, nil
}

// Search performs a semantic search.
func (s *SearchService) Search(query string, k int) (*SearchResponse, error) {
	if k <= 0 {
		k = DefaultSearchK
	}

	// Embed query
	queryVector := DeterministicEmbed(query, s.embeddingSize)

	// Search in Qdrant
	results, err := s.qdrant.Search(QdrantCollection, queryVector, k)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert results
	searchResults := make([]SearchResult, 0, len(results))
	for _, r := range results {
		sr := SearchResult{
			Score: r.Score,
		}

		if payload, ok := r.Payload.(map[string]interface{}); ok {
			if path, ok := payload["path"].(string); ok {
				sr.Path = path
			}
			if chunkIdx, ok := payload["chunk_index"].(float64); ok {
				sr.ChunkIndex = int(chunkIdx)
			}
			if sha, ok := payload["sha256"].(string); ok {
				sr.SHA256 = sha
			}
			if snippet, ok := payload["snippet"].(string); ok {
				sr.Snippet = snippet
			}
			if content, ok := payload["content"].(string); ok {
				sr.Content = content
			}
		}

		searchResults = append(searchResults, sr)
	}

	return &SearchResponse{
		Query:   query,
		K:       k,
		Results: searchResults,
	}, nil
}

// DeterministicEmbed creates a deterministic embedding from content.
// This is a stub that can be replaced with real embeddings later.
// It uses SHA-256 hash to generate a reproducible vector.
func DeterministicEmbed(content string, size int) []float32 {
	hash := sha256.Sum256([]byte(content))
	vector := make([]float32, size)

	// Use hash bytes to generate floats in [-1, 1]
	for i := 0; i < size; i++ {
		b := hash[i%len(hash)]
		vector[i] = (float32(b) / 127.5) - 1.0
	}

	// Normalize the vector
	var norm float32
	for _, v := range vector {
		norm += v * v
	}
	if norm > 0 {
		norm = float32(1.0 / float64(norm))
		for i := range vector {
			vector[i] *= norm
		}
	}

	return vector
}

// ChunkText splits text into overlapping chunks.
func ChunkText(text string, chunkSize, overlap int) []string {
	if len(text) == 0 {
		return nil
	}

	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 2
	}

	var chunks []string
	start := 0
	for start < len(text) {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}

		chunk := text[start:end]
		chunks = append(chunks, chunk)

		if end >= len(text) {
			break
		}

		start = end - overlap
	}

	return chunks
}

// isTextFile checks if content appears to be text.
func isTextFile(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	// Check first 512 bytes for null bytes (common in binary files)
	checkLen := 512
	if len(content) < checkLen {
		checkLen = len(content)
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return false
		}
	}
	return true
}

// truncateSnippet truncates text to maxLen, adding ellipsis if needed.
func truncateSnippet(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

// QdrantClient is a simple HTTP client for Qdrant.
type QdrantClient struct {
	baseURL    string
	httpClient *http.Client
}

// QdrantPoint represents a point to upsert.
type QdrantPoint struct {
	ID      string                 `json:"id"`
	Vector  []float32              `json:"vector"`
	Payload map[string]interface{} `json:"payload"`
}

// QdrantSearchResult represents a search result from Qdrant.
type QdrantSearchResult struct {
	ID      string      `json:"id"`
	Score   float32     `json:"score"`
	Payload interface{} `json:"payload"`
}

// UpsertPoints upserts points to a collection.
func (c *QdrantClient) UpsertPoints(collection string, points []QdrantPoint) error {
	body, _ := json.Marshal(map[string]interface{}{
		"points": points,
	})

	req, _ := http.NewRequest("PUT", c.baseURL+"/collections/"+collection+"/points", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upsert failed: %s", string(respBody))
	}

	return nil
}

// Search performs a vector search.
func (c *QdrantClient) Search(collection string, vector []float32, limit int) ([]QdrantSearchResult, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
	})

	req, _ := http.NewRequest("POST", c.baseURL+"/collections/"+collection+"/points/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed: %s", string(respBody))
	}

	var result struct {
		Result []QdrantSearchResult `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Result, nil
}
