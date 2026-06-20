package llm

import "context"

// LLMRequest is a single completion request. The system prompt is the stable,
// cacheable part; the user prompt is the per-call payload. Sensor identifies the
// caller so the client can route to a per-sensor model.
type LLMRequest struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
	Sensor       string
}

// LLMResponse is the model's reply plus token accounting for cost reporting.
// Cached is true when the provider served the system prompt from its prompt
// cache.
type LLMResponse struct {
	Content   string
	TokensIn  int
	TokensOut int
	Cached    bool
}

// LLMClient is the provider-agnostic LLM client. Implementations own prompt
// caching and (later) batching.
type LLMClient interface {
	Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
	Provider() string
	Model() string
}
