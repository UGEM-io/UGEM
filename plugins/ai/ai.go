// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package ai

import (
	"context"
	"fmt"
	"github.com/ugem-io/ugem/runtime"
	"os"
)

type AIPlugin struct {
	apiKey string
}

func (a *AIPlugin) Name() string {
	return "ai"
}

func (a *AIPlugin) Init(ctx context.Context, config map[string]string) error {
	a.apiKey = config["api_key"]
	if a.apiKey == "" {
		a.apiKey = os.Getenv("OPENAI_API_KEY")
	}
	return nil
}

func (a *AIPlugin) Actions() map[string]runtime.ActionHandler {
	return map[string]runtime.ActionHandler{
		"ai.process":   a.ProcessAction,
		"ai.summarize": a.SummarizeAction,
	}
}

func (a *AIPlugin) ProcessAction(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
	prompt, _ := input["prompt"].(string)
	model, _ := input["model"].(string)
	
	if model == "" {
		model = "gpt-4"
	}

	// Mock response for now (would call OpenAI in real implementation)
	return map[string]interface{}{
		"model":    model,
		"response": fmt.Sprintf("AI response to: %s", prompt),
		"status":   "processed",
	}, nil
}

func (a *AIPlugin) SummarizeAction(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
	text, _ := input["text"].(string)
	
	return map[string]interface{}{
		"summary": fmt.Sprintf("Summary of: %s", text),
		"status":  "summarized",
	}, nil
}
