package main

import (
	"testing"
)

func TestDeterministicEmbed_Length(t *testing.T) {
	sizes := []int{64, 128, 256, 512}
	for _, size := range sizes {
		vector := DeterministicEmbed("test content", size)
		if len(vector) != size {
			t.Errorf("Expected vector length %d, got %d", size, len(vector))
		}
	}
}

func TestDeterministicEmbed_Determinism(t *testing.T) {
	content := "This is a test string for embedding"
	v1 := DeterministicEmbed(content, 128)
	v2 := DeterministicEmbed(content, 128)

	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("Vectors differ at index %d: %f != %f", i, v1[i], v2[i])
		}
	}
}

func TestDeterministicEmbed_DifferentContent(t *testing.T) {
	v1 := DeterministicEmbed("content A", 128)
	v2 := DeterministicEmbed("content B", 128)

	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("Different content should produce different vectors")
	}
}

func TestDeterministicEmbed_Range(t *testing.T) {
	vector := DeterministicEmbed("test content", 128)
	for i, v := range vector {
		if v < -2.0 || v > 2.0 {
			t.Errorf("Vector value at index %d out of expected range: %f", i, v)
		}
	}
}

func TestChunkText_SingleChunk(t *testing.T) {
	text := "Short text"
	chunks := ChunkText(text, 100, 20)
	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("Expected chunk to equal input text")
	}
}

func TestChunkText_MultipleChunks(t *testing.T) {
	text := "0123456789" // 10 chars
	chunks := ChunkText(text, 4, 2)

	// Expected: [0123, 2345, 4567, 6789]
	if len(chunks) < 2 {
		t.Errorf("Expected multiple chunks, got %d", len(chunks))
	}

	// First chunk should start at 0
	if chunks[0] != "0123" {
		t.Errorf("First chunk should be '0123', got '%s'", chunks[0])
	}
}

func TestChunkText_EmptyText(t *testing.T) {
	chunks := ChunkText("", 100, 20)
	if chunks != nil {
		t.Errorf("Expected nil for empty text, got %v", chunks)
	}
}

func TestChunkText_OverlapGreaterThanSize(t *testing.T) {
	text := "0123456789"
	// When overlap >= chunkSize, it should be adjusted to chunkSize/2
	chunks := ChunkText(text, 4, 10)

	// Should still produce chunks without infinite loop
	if len(chunks) == 0 {
		t.Error("Should produce at least one chunk")
	}
}

func TestTruncateSnippet(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is..."},
		{"exactly10!", 10, "exactly10!"},
	}

	for _, tt := range tests {
		result := truncateSnippet(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateSnippet(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestIsTextFile(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		expected bool
	}{
		{"empty", []byte{}, true},
		{"text", []byte("Hello, World!"), true},
		{"binary", []byte{0x00, 0x01, 0x02}, false},
		{"text with newlines", []byte("line1\nline2\n"), true},
	}

	for _, tt := range tests {
		result := isTextFile(tt.content)
		if result != tt.expected {
			t.Errorf("isTextFile(%s) = %v, want %v", tt.name, result, tt.expected)
		}
	}
}
