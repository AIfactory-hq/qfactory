// Package modelruntime provides model runtime abstractions for LLM integration.
package modelruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// Runtime defines the interface for model providers.
type Runtime interface {
	Complete(ctx context.Context, req contracts.CompletionRequest) (*contracts.CompletionResponse, error)
	Health(ctx context.Context) (*contracts.HealthStatus, error)
}

// OllamaRuntime implements Runtime using the Ollama HTTP API.
type OllamaRuntime struct {
	baseURL      string
	defaultModel string
	timeout      time.Duration
	httpClient   *http.Client
}

// OllamaConfig holds configuration for OllamaRuntime.
type OllamaConfig struct {
	URL          string
	DefaultModel string
	TimeoutSec   int
}

// LoadOllamaConfigFromEnv loads Ollama configuration from environment variables.
func LoadOllamaConfigFromEnv() OllamaConfig {
	cfg := OllamaConfig{
		URL:          "http://localhost:11434",
		DefaultModel: "llama3.1:8b",
		TimeoutSec:   60,
	}

	if url := os.Getenv("OLLAMA_URL"); url != "" {
		cfg.URL = url
	}
	if model := os.Getenv("OLLAMA_DEFAULT_MODEL"); model != "" {
		cfg.DefaultModel = model
	}
	if ts := os.Getenv("MODEL_REQUEST_TIMEOUT_SECONDS"); ts != "" {
		if v, err := strconv.Atoi(ts); err == nil && v > 0 {
			cfg.TimeoutSec = v
		}
	}

	return cfg
}

// NewOllamaRuntime creates a new Ollama runtime.
func NewOllamaRuntime(cfg OllamaConfig) *OllamaRuntime {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	return &OllamaRuntime{
		baseURL:      cfg.URL,
		defaultModel: cfg.DefaultModel,
		timeout:      timeout,
		httpClient:   &http.Client{Timeout: timeout},
	}
}

// ollamaGenerateRequest is the request body for Ollama /api/generate.
type ollamaGenerateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	System  string                 `json:"system,omitempty"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

// ollamaGenerateResponse is the response from Ollama /api/generate.
type ollamaGenerateResponse struct {
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	PromptEvalCount int    `json:"prompt_eval_count,omitempty"`
	EvalCount       int    `json:"eval_count,omitempty"`
}

// Complete sends a completion request to Ollama.
func (o *OllamaRuntime) Complete(ctx context.Context, req contracts.CompletionRequest) (*contracts.CompletionResponse, error) {
	startTime := time.Now()

	model := req.Model
	if model == "" {
		model = o.defaultModel
	}

	ollamaReq := ollamaGenerateRequest{
		Model:  model,
		Prompt: req.Prompt,
		System: req.System,
		Stream: false,
	}

	// Build options
	opts := make(map[string]interface{})
	if req.MaxOutputTokens > 0 {
		opts["num_predict"] = req.MaxOutputTokens
	}
	if req.Temperature > 0 {
		opts["temperature"] = req.Temperature
	}
	if len(opts) > 0 {
		ollamaReq.Options = opts
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	latencyMs := time.Since(startTime).Milliseconds()

	// Build usage - estimate if Ollama doesn't provide counts
	usage := contracts.TokenUsage{}
	if ollamaResp.PromptEvalCount > 0 || ollamaResp.EvalCount > 0 {
		usage.InputTokens = ollamaResp.PromptEvalCount
		usage.OutputTokens = ollamaResp.EvalCount
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	} else {
		// Estimate using chars/4 heuristic
		usage.InputTokens = (len(req.Prompt) + len(req.System)) / 4
		usage.OutputTokens = len(ollamaResp.Response) / 4
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		usage.Estimated = true
	}

	return &contracts.CompletionResponse{
		Text:      ollamaResp.Response,
		Usage:     usage,
		Model:     model,
		Provider:  string(contracts.ModelProviderOllama),
		LatencyMs: latencyMs,
	}, nil
}

// Health checks the health of the Ollama service.
func (o *OllamaRuntime) Health(ctx context.Context) (*contracts.HealthStatus, error) {
	status := &contracts.HealthStatus{
		Provider:  string(contracts.ModelProviderOllama),
		CheckedAt: time.Now().UTC(),
		Details:   make(map[string]string),
	}

	status.Details["url"] = o.baseURL
	status.Details["default_model"] = o.defaultModel

	// Check Ollama API health
	httpReq, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/api/tags", nil)
	if err != nil {
		status.OK = false
		status.Details["error"] = err.Error()
		return status, nil
	}

	resp, err := o.httpClient.Do(httpReq)
	if err != nil {
		status.OK = false
		status.Details["error"] = err.Error()
		return status, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		status.OK = false
		status.Details["error"] = fmt.Sprintf("status %d", resp.StatusCode)
		return status, nil
	}

	status.OK = true
	status.Details["status"] = "connected"
	return status, nil
}

// MockRuntime implements Runtime with deterministic responses for testing.
type MockRuntime struct {
	ResponsePrefix string
	LatencyMs      int64
}

// NewMockRuntime creates a new mock runtime.
func NewMockRuntime() *MockRuntime {
	return &MockRuntime{
		ResponsePrefix: "Mock response: ",
		LatencyMs:      10,
	}
}

// Complete returns a deterministic echo response.
func (m *MockRuntime) Complete(ctx context.Context, req contracts.CompletionRequest) (*contracts.CompletionResponse, error) {
	// Simulate latency
	time.Sleep(time.Duration(m.LatencyMs) * time.Millisecond)

	responseText := m.ResponsePrefix + req.Prompt
	if req.MaxOutputTokens > 0 && len(responseText) > req.MaxOutputTokens*4 {
		responseText = responseText[:req.MaxOutputTokens*4]
	}

	inputTokens := (len(req.Prompt) + len(req.System)) / 4
	outputTokens := len(responseText) / 4

	return &contracts.CompletionResponse{
		Text: responseText,
		Usage: contracts.TokenUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
			Estimated:    true,
		},
		Model:     "mock",
		Provider:  string(contracts.ModelProviderMock),
		LatencyMs: m.LatencyMs,
	}, nil
}

// Health returns healthy status for mock runtime.
func (m *MockRuntime) Health(ctx context.Context) (*contracts.HealthStatus, error) {
	return &contracts.HealthStatus{
		OK:        true,
		Provider:  string(contracts.ModelProviderMock),
		CheckedAt: time.Now().UTC(),
		Details: map[string]string{
			"status": "mock runtime always healthy",
		},
	}, nil
}
