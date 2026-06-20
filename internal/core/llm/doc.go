// Package llm defines the provider-agnostic LLM client used by sensors for the
// semantic layer of the pyramid. It is the single seam behind which prompt
// caching and batching (PRD section 15) live, so a new language inherits both
// for free.
//
// It defines the [LLMClient] contract and its request/response shapes, plus an
// [AnthropicClient] that talks to the Messages API over HTTP and marks the
// system prompt cacheable. Other providers (OpenAI, Ollama, ...) come later.
package llm
