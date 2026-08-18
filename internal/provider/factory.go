package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/openai"
)

// Factory creates fantasy agents from config using catwalk provider definitions
type Factory struct {
	providers map[catwalk.InferenceProvider]catwalk.Provider
}

// NewFactory creates a new provider factory
func NewFactory() *Factory {
	f := &Factory{
		providers: make(map[catwalk.InferenceProvider]catwalk.Provider),
	}
	f.loadProviders()
	return f
}

// loadProviders loads all providers from catwalk's config files
func (f *Factory) loadProviders() {
	// Find catwalk module path
	catwalkPath := findCatwalkPath()
	if catwalkPath == "" {
		// Fallback to basic providers
		f.loadBasicProviders()
		return
	}

	configsDir := filepath.Join(catwalkPath, "internal", "providers", "configs")
	
	entries, err := os.ReadDir(configsDir)
	if err != nil {
		f.loadBasicProviders()
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(configsDir, entry.Name()))
		if err != nil {
			continue
		}

		var p catwalk.Provider
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}

		if p.ID != "" {
			f.providers[p.ID] = p
		}
	}
}

// findCatwalkPath finds the catwalk module cache path
func findCatwalkPath() string {
	// Try to find catwalk from go mod cache
	paths := []string{
		filepath.Join(os.Getenv("HOME"), "go", "pkg", "mod", "charm.land", "catwalk@v0.51.27"),
		filepath.Join(os.Getenv("GOPATH"), "pkg", "mod", "charm.land", "catwalk@v0.51.27"),
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// loadBasicProviders loads basic hardcoded providers as fallback
func (f *Factory) loadBasicProviders() {
	// Minimal fallback - these should be loaded from catwalk configs
}

// resolveEnvVar resolves $VAR or ${VAR} patterns in a string
func resolveEnvVar(s string) string {
	re := regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[1:]
		if match[1] == '{' {
			varName = match[2 : len(match)-1]
		}
		return os.Getenv(varName)
	})
}

// CreateProvider creates a fantasy provider from agent config
func (f *Factory) CreateProvider(providerName, modelName, apiKey string) (fantasy.LanguageModel, error) {
	ctx := context.Background()

	// Look up provider config by name
	providerID := catwalk.InferenceProvider(providerName)
	providerConfig, ok := f.providers[providerID]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s (available: %v)", providerName, f.availableProviders())
	}

	// Resolve API key
	resolvedAPIKey := apiKey
	if resolvedAPIKey == "" {
		if providerConfig.APIKey != "" {
			resolvedAPIKey = resolveEnvVar(providerConfig.APIKey)
		}
	}

	// Use default model if not specified
	if modelName == "" {
		modelName = providerConfig.DefaultLargeModelID
		if modelName == "" {
			modelName = providerConfig.DefaultSmallModelID
		}
	}

	// Create provider based on type
	switch providerConfig.Type {
	case catwalk.TypeAnthropic:
		return f.createAnthropicProvider(ctx, providerConfig, resolvedAPIKey, modelName)
	case catwalk.TypeOpenAI, catwalk.TypeOpenAICompat, "":
		return f.createOpenAIProvider(ctx, providerConfig, resolvedAPIKey, modelName)
	default:
		return f.createOpenAIProvider(ctx, providerConfig, resolvedAPIKey, modelName)
	}
}

// createAnthropicProvider creates an Anthropic-type provider
func (f *Factory) createAnthropicProvider(ctx context.Context, config catwalk.Provider, apiKey, modelName string) (fantasy.LanguageModel, error) {
	baseURL := strings.TrimSuffix(config.APIEndpoint, "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	provider, err := anthropic.New(
		anthropic.WithAPIKey(apiKey),
		anthropic.WithBaseURL(baseURL),
	)
	if err != nil {
		return nil, fmt.Errorf("anthropic provider: %w", err)
	}
	return provider.LanguageModel(ctx, modelName)
}

// createOpenAIProvider creates an OpenAI-compatible provider
func (f *Factory) createOpenAIProvider(ctx context.Context, config catwalk.Provider, apiKey, modelName string) (fantasy.LanguageModel, error) {
	baseURL := strings.TrimSuffix(config.APIEndpoint, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	provider, err := openai.New(
		openai.WithAPIKey(apiKey),
		openai.WithBaseURL(baseURL),
	)
	if err != nil {
		return nil, fmt.Errorf("openai provider: %w", err)
	}
	return provider.LanguageModel(ctx, modelName)
}

// availableProviders returns a list of available provider IDs
func (f *Factory) availableProviders() []string {
	ids := make([]string, 0, len(f.providers))
	for id := range f.providers {
		ids = append(ids, string(id))
	}
	return ids
}

// ProviderInfo returns information about a provider
type ProviderInfo struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Models []string `json:"models"`
}

// ListProviders returns all available providers
func (f *Factory) ListProviders() []ProviderInfo {
	infos := make([]ProviderInfo, 0, len(f.providers))
	for _, p := range f.providers {
		models := make([]string, len(p.Models))
		for i, m := range p.Models {
			models[i] = m.ID
		}
		infos = append(infos, ProviderInfo{
			ID:     string(p.ID),
			Name:   p.Name,
			Type:   string(p.Type),
			Models: models,
		})
	}
	return infos
}
