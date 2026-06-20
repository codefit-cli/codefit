package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const cannedAnthropicResponse = `{
  "content": [{"type": "text", "text": "hello from claude"}],
  "usage": {"input_tokens": 10, "output_tokens": 5, "cache_read_input_tokens": 10}
}`

func newTestClient(handler http.HandlerFunc) (*AnthropicClient, *httptest.Server) {
	srv := httptest.NewServer(handler)
	c := &AnthropicClient{
		apiKey:     "sk-ant-test",
		model:      "claude-sonnet-4-6",
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}
	return c, srv
}

func TestAnthropicComplete(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant-test" {
			t.Errorf("x-api-key = %q, want sk-ant-test", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("missing anthropic-version header")
		}
		io.WriteString(w, cannedAnthropicResponse)
	})
	defer srv.Close()

	resp, err := c.Complete(context.Background(), LLMRequest{
		SystemPrompt: "you are a security auditor",
		UserPrompt:   "audit this",
		MaxTokens:    1024,
		Sensor:       "security",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello from claude" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.TokensIn != 10 || resp.TokensOut != 5 {
		t.Errorf("tokens in/out = %d/%d, want 10/5", resp.TokensIn, resp.TokensOut)
	}
	if !resp.Cached {
		t.Error("Cached should be true when cache_read_input_tokens > 0")
	}
	if c.Provider() != "anthropic" || c.Model() != "claude-sonnet-4-6" {
		t.Errorf("Provider/Model = %q/%q", c.Provider(), c.Model())
	}
}

func TestAnthropicSendsPromptCaching(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("request body not JSON: %v", err)
		}
		// system must be an array of blocks carrying cache_control.
		if !strings.Contains(string(body), "cache_control") {
			t.Errorf("request does not mark the system prompt cacheable:\n%s", body)
		}
		io.WriteString(w, cannedAnthropicResponse)
	})
	defer srv.Close()

	if _, err := c.Complete(context.Background(), LLMRequest{
		SystemPrompt: "stable system prompt",
		UserPrompt:   "x",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAnthropicAPIError(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"bad request"}}`)
	})
	defer srv.Close()

	if _, err := c.Complete(context.Background(), LLMRequest{UserPrompt: "x"}); err == nil {
		t.Error("Complete should return an error on a non-2xx response")
	}
}
