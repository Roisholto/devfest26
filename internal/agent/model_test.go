package agent

import (
	"context"
	"testing"
)

func TestNewGeminiRequiresWorkshopCredential(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")

	llm, err := NewGemini(context.Background(), "test-model")

	if err == nil || llm != nil {
		t.Fatal("model must reject a missing GEMINI_API_KEY before SDK authentication")
	}
}
