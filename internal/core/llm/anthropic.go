package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	anthropicBaseURL    = "https://api.anthropic.com"
	anthropicAPIVersion = "2023-06-01"
	defaultMaxTokens    = 4096
)

// AnthropicClient talks to the Anthropic Messages API over HTTP. It marks the
// system prompt as cacheable (cache_control) so the stateless MCP overhead is
// neutralized by prompt caching (PRD §15).
type AnthropicClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewAnthropicClient builds a client for the given key and model.
func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	return &AnthropicClient{
		apiKey:     apiKey,
		model:      model,
		baseURL:    anthropicBaseURL,
		httpClient: http.DefaultClient,
	}
}

func (c *AnthropicClient) Provider() string { return "anthropic" }
func (c *AnthropicClient) Model() string    { return c.model }

// wire types for the Messages API ------------------------------------------

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	System    []anthropicSystemBlock `json:"system,omitempty"`
	Messages  []anthropicMessage     `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens          int `json:"input_tokens"`
		OutputTokens         int `json:"output_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// Complete sends one request to the Messages API.
func (c *AnthropicClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	payload := anthropicRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		Messages:  []anthropicMessage{{Role: "user", Content: req.UserPrompt}},
	}
	if req.SystemPrompt != "" {
		payload.System = []anthropicSystemBlock{{
			Type:         "text",
			Text:         req.SystemPrompt,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		}}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("encoding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("building request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("calling Anthropic API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return LLMResponse{}, fmt.Errorf("anthropic API returned %d: %s", resp.StatusCode, respBody)
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return LLMResponse{}, fmt.Errorf("decoding response: %w", err)
	}

	var content string
	if len(parsed.Content) > 0 {
		content = parsed.Content[0].Text
	}
	return LLMResponse{
		Content:   content,
		TokensIn:  parsed.Usage.InputTokens,
		TokensOut: parsed.Usage.OutputTokens,
		Cached:    parsed.Usage.CacheReadInputTokens > 0,
	}, nil
}
