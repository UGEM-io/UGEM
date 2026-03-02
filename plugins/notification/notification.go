// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package notification

import (
	"context"
	"github.com/ugem-io/ugem/runtime"
	"log"
)

type ConsoleNotifier struct {
}

func (c *ConsoleNotifier) Name() string {
	return "notification"
}

func (c *ConsoleNotifier) Init(ctx context.Context, config map[string]string) error {
	return nil
}

func (c *ConsoleNotifier) Notify(ctx context.Context, message string, target map[string]string) error {
	log.Printf("[NOTIFICATION] [%v] %s", target, message)
	return nil
}

func (c *ConsoleNotifier) Actions() map[string]runtime.ActionHandler {
	return map[string]runtime.ActionHandler{
		"notify.send": c.SendAction,
	}
}

// Action handlers for ActionDispatcher
func (c *ConsoleNotifier) SendAction(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
	message, _ := input["message"].(string)
	target, _ := input["target"].(map[string]string)
	
	if err := c.Notify(ctx.CancelCtx, message, target); err != nil {
		return nil, err
	}
	
	return map[string]interface{}{"status": "sent"}, nil
}
