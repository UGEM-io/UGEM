// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package gdl

import (
	"fmt"
	"time"

	"github.com/ugem-io/ugem/runtime"
)

type HTTPAction struct{}

func NewHTTPAction() *HTTPAction {
	return &HTTPAction{}
}

func (a *HTTPAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"status": 200, "body": "ok"}, nil
}

type DBAction struct{}

func NewDBAction() *DBAction {
	return &DBAction{}
}

func (a *DBAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"rows": []interface{}{}, "error": nil}, nil
}

type EmailAction struct{}

func NewEmailAction() *EmailAction {
	return &EmailAction{}
}

func (a *EmailAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"sent": true, "error": nil}, nil
}

type SMSAction struct{}

func NewSMSAction() *SMSAction {
	return &SMSAction{}
}

func (a *SMSAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"sid": "SM" + fmt.Sprintf("%d", time.Now().Unix()), "status": "sent", "error": nil}, nil
}

type PaymentAction struct{}

func NewPaymentAction() *PaymentAction {
	return &PaymentAction{}
}

func (a *PaymentAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	amount, _ := input["amount"].(float64)
	currency, _ := input["currency"].(string)
	if currency == "" {
		currency = "usd"
	}
	return map[string]interface{}{
		"id":     "pi_" + fmt.Sprintf("%d", time.Now().Unix()),
		"amount": amount, "currency": currency, "status": "succeeded",
	}, nil
}

type TimerAction struct{}

func NewTimerAction() *TimerAction {
	return &TimerAction{}
}

func (a *TimerAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	duration, _ := input["duration"].(int)
	if duration <= 0 {
		duration = 100
	}
	time.Sleep(time.Duration(duration) * time.Millisecond)
	return map[string]interface{}{"elapsed": true}, nil
}

type FileAction struct{}

func NewFileAction() *FileAction {
	return &FileAction{}
}

func (a *FileAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"url": "file:///test"}, nil
}

type QueueAction struct{}

func NewQueueAction() *QueueAction {
	return &QueueAction{}
}

func (a *QueueAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"published": true, "message_id": "msg_" + fmt.Sprintf("%d", time.Now().Unix())}, nil
}

type AIAction struct{}

func NewAIAction() *AIAction {
	return &AIAction{}
}

func (a *AIAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"content": "AI response", "model": "gpt-4"}, nil
}

type LogAction struct{}

func NewLogAction() *LogAction {
	return &LogAction{}
}

func (a *LogAction) Execute(ctx runtime.ActionContext, input map[string]interface{}) (map[string]interface{}, error) {
	message, _ := input["message"].(string)
	fmt.Printf("[log] %s\n", message)
	return map[string]interface{}{"logged": true}, nil
}

func RegisterAllActions(planner runtime.Planner) {
	planner.RegisterAction("http.call", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewHTTPAction().Execute(ctx, input)
	})
	planner.RegisterAction("api.call", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewHTTPAction().Execute(ctx, input)
	})
	planner.RegisterAction("db.query", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewDBAction().Execute(ctx, input)
	})
	planner.RegisterAction("email.send", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewEmailAction().Execute(ctx, input)
	})
	planner.RegisterAction("sms.send", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewSMSAction().Execute(ctx, input)
	})
	planner.RegisterAction("payment.charge", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewPaymentAction().Execute(ctx, input)
	})
	planner.RegisterAction("timer.wait", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewTimerAction().Execute(ctx, input)
	})
	planner.RegisterAction("file.upload", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewFileAction().Execute(ctx, input)
	})
	planner.RegisterAction("queue.publish", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewQueueAction().Execute(ctx, input)
	})
	planner.RegisterAction("ai.call", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewAIAction().Execute(ctx, input)
	})
	planner.RegisterAction("log", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
		return NewLogAction().Execute(ctx, input)
	})
}
