package main

import (
	"strings"
	"testing"
)

func TestValidateAICompletionRequest(t *testing.T) {
	tests := []struct {
		name string
		req  aiCompletionReq
		ok   bool
	}{
		{name: "valid", req: aiCompletionReq{PromptKey: "rewrite", Content: "hello"}, ok: true},
		{name: "missing prompt", req: aiCompletionReq{Content: "hello"}},
		{name: "missing content", req: aiCompletionReq{PromptKey: "rewrite", Content: "  "}},
		{name: "prompt too large", req: aiCompletionReq{PromptKey: strings.Repeat("a", maxAIPromptKeyBytes+1), Content: "hello"}},
		{name: "content too large", req: aiCompletionReq{PromptKey: "rewrite", Content: strings.Repeat("a", maxAIContentBytes+1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAICompletionRequest(tt.req)
			if (err == nil) != tt.ok {
				t.Fatalf("validation error = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}
