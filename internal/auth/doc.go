// Package auth manages LLM provider credentials. API keys are stored in the OS
// keychain (go-keyring) with an encrypted-file fallback, and env vars
// (ANTHROPIC_API_KEY, ...) are honored for CI. Keys are never written to
// .codefit.yaml (PRD section 11).
//
// Skeleton: this declares the [Store] contract. No keychain backend is
// implemented yet.
package auth
