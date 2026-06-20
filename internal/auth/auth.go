package auth

// Credential identifies a stored provider credential.
type Credential struct {
	Provider string
	Model    string
	// BaseURL is set for local providers (Ollama, LM Studio); empty for cloud.
	BaseURL string
}

// Store persists and retrieves provider credentials from the OS keychain (with
// an encrypted-file fallback). API keys never transit .codefit.yaml.
//
// Skeleton: no implementation yet.
type Store interface {
	// Set stores the API key for a provider in the keychain.
	Set(provider, apiKey string) error
	// Get retrieves the API key for a provider (keychain, then env var).
	Get(provider string) (string, error)
	// Delete removes a provider's credential.
	Delete(provider string) error
}
