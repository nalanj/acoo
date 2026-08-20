package provider

import (
	"testing"
)

func TestFactoryListProviders(t *testing.T) {
	factory := NewFactory()
	providers := factory.ListProviders()

	if len(providers) == 0 {
		t.Error("ListProviders() returned empty list")
	}

	// Check that common providers are present
	hasOpenAI := false
	hasAnthropic := false
	for _, p := range providers {
		if p.ID == "openai" {
			hasOpenAI = true
		}
		if p.ID == "anthropic" {
			hasAnthropic = true
		}
	}

	if !hasOpenAI {
		t.Error("OpenAI provider not found")
	}
	if !hasAnthropic {
		t.Error("Anthropic provider not found")
	}
}

func TestProviderInfoFields(t *testing.T) {
	factory := NewFactory()
	providers := factory.ListProviders()

	if len(providers) == 0 {
		t.Skip("No providers available")
	}

	// Check that provider info has expected fields
	p := providers[0]
	if p.ID == "" {
		t.Error("Provider ID is empty")
	}
	if p.Name == "" {
		t.Error("Provider Name is empty")
	}
}
