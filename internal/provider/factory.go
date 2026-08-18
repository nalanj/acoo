package provider

import (
	"context"
	"fmt"
	"os"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openrouter"
)

// Factory creates fantasy agents from config
type Factory struct{}

// NewFactory creates a new provider factory
func NewFactory() *Factory {
	return &Factory{}
}

// CreateProvider creates a fantasy provider from agent config
func (f *Factory) CreateProvider(providerName, modelName, apiKey string) (fantasy.LanguageModel, error) {
	ctx := context.Background()

	switch providerName {
	case "openai":
		return f.createOpenAI(ctx, modelName, apiKey)
	case "openrouter":
		return f.createOpenRouter(ctx, modelName, apiKey)
	case "anthropic":
		return f.createAnthropic(ctx, modelName, apiKey)
	case "google", "gemini":
		return f.createGoogle(ctx, modelName, apiKey)
	default:
		return f.createOpenAICompatible(ctx, providerName, modelName, apiKey)
	}
}

// createOpenAI creates an OpenAI provider
func (f *Factory) createOpenAI(ctx context.Context, modelName, apiKey string) (fantasy.LanguageModel, error) {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if modelName == "" {
		modelName = "gpt-4o"
	}
	provider, err := openai.New(openai.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("openai provider: %w", err)
	}
	return provider.LanguageModel(ctx, modelName)
}

// createOpenRouter creates an OpenRouter provider
func (f *Factory) createOpenRouter(ctx context.Context, modelName, apiKey string) (fantasy.LanguageModel, error) {
	if apiKey == "" {
		apiKey = os.Getenv("OPENROUTER_API_KEY")
	}
	if modelName == "" {
		modelName = "moonshotai/kimi-k2"
	}
	provider, err := openrouter.New(openrouter.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("openrouter provider: %w", err)
	}
	return provider.LanguageModel(ctx, modelName)
}

// createAnthropic creates an Anthropic provider
func (f *Factory) createAnthropic(ctx context.Context, modelName, apiKey string) (fantasy.LanguageModel, error) {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if modelName == "" {
		modelName = "claude-3-5-sonnet-20241022"
	}
	// Use openaicompat with Anthropic endpoint
	provider, err := openai.New(
		openai.WithAPIKey(apiKey),
	)
	if err != nil {
		return nil, fmt.Errorf("anthropic provider: %w", err)
	}
	return provider.LanguageModel(ctx, modelName)
}

// createGoogle creates a Google provider
func (f *Factory) createGoogle(ctx context.Context, modelName, apiKey string) (fantasy.LanguageModel, error) {
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if modelName == "" {
		modelName = "gemini-2.0-flash"
	}
	provider, err := openai.New(
		openai.WithAPIKey(apiKey),
	)
	if err != nil {
		return nil, fmt.Errorf("google provider: %w", err)
	}
	return provider.LanguageModel(ctx, modelName)
}

// createOpenAICompatible creates an OpenAI-compatible provider
func (f *Factory) createOpenAICompatible(ctx context.Context, endpoint, modelName, apiKey string) (fantasy.LanguageModel, error) {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if modelName == "" {
		modelName = "gpt-4"
	}
	provider, err := openai.New(
		openai.WithAPIKey(apiKey),
	)
	if err != nil {
		return nil, fmt.Errorf("openaicompat provider: %w", err)
	}
	return provider.LanguageModel(ctx, modelName)
}
