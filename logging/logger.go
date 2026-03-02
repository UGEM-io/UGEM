// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package logging

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
	LevelFatal Level = "fatal"
)

type Field map[string]interface{}

type LogEntry struct {
	Timestamp     string                 `json:"timestamp"`
	Level         Level                  `json:"level"`
	Message       string                 `json:"message"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Component     string                 `json:"component,omitempty"`
	Fields        map[string]interface{} `json:"fields,omitempty"`
}

type Logger struct {
	mu           sync.RWMutex
	output       io.Writer
	minLevel     Level
	component    string
	enableColors bool
}

var (
	defaultLogger *Logger
	once          sync.Once
)

func Init(level Level, component string) {
	once.Do(func() {
		defaultLogger = &Logger{
			output:       os.Stdout,
			minLevel:     level,
			component:    component,
			enableColors: true,
		}
	})
}

func Default() *Logger {
	once.Do(func() {
		defaultLogger = &Logger{
			output:   os.Stdout,
			minLevel: LevelInfo,
		}
	})
	return defaultLogger
}

func SetOutput(w io.Writer) {
	Default().SetOutput(w)
}

func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
}

func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minLevel = level
}

func (l *Logger) SetComponent(component string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.component = component
}

func (l *Logger) log(level Level, msg string, correlationID string, fields Field) {
	if !l.shouldLog(level) {
		return
	}

	entry := LogEntry{
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		Level:         level,
		Message:       msg,
		CorrelationID: correlationID,
		Component:     l.component,
		Fields:        fields,
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	l.output.Write(append(data, '\n'))
}

func (l *Logger) shouldLog(level Level) bool {
	levels := map[Level]int{
		LevelDebug: 0,
		LevelInfo:  1,
		LevelWarn:  2,
		LevelError: 3,
		LevelFatal: 4,
	}

	current, ok := levels[l.minLevel]
	if !ok {
		current = 1
	}

	incoming, ok := levels[level]
	if !ok {
		incoming = 1
	}

	return incoming >= current
}

func (l *Logger) Debug(msg string, fields Field) {
	l.log(LevelDebug, msg, "", fields)
}

func (l *Logger) Info(msg string, fields Field) {
	l.log(LevelInfo, msg, "", fields)
}

func (l *Logger) Warn(msg string, fields Field) {
	l.log(LevelWarn, msg, "", fields)
}

func (l *Logger) Error(msg string, fields Field) {
	l.log(LevelError, msg, "", fields)
}

func (l *Logger) Fatal(msg string, fields Field) {
	l.log(LevelFatal, msg, "", fields)
	os.Exit(1)
}

func (l *Logger) WithCorrelationID(id string) *CorrelationLogger {
	return &CorrelationLogger{
		logger:        l,
		correlationID: id,
	}
}

type CorrelationLogger struct {
	logger        *Logger
	correlationID string
}

func (c *CorrelationLogger) Debug(msg string, fields Field) {
	c.logger.log(LevelDebug, msg, c.correlationID, fields)
}

func (c *CorrelationLogger) Info(msg string, fields Field) {
	c.logger.log(LevelInfo, msg, c.correlationID, fields)
}

func (c *CorrelationLogger) Warn(msg string, fields Field) {
	c.logger.log(LevelWarn, msg, c.correlationID, fields)
}

func (c *CorrelationLogger) Error(msg string, fields Field) {
	c.logger.log(LevelError, msg, c.correlationID, fields)
}

func (c *CorrelationLogger) Fatal(msg string, fields Field) {
	c.logger.log(LevelFatal, msg, c.correlationID, fields)
}

func Debug(msg string, fields Field) {
	Default().Debug(msg, fields)
}

func Info(msg string, fields Field) {
	Default().Info(msg, fields)
}

func Warn(msg string, fields Field) {
	Default().Warn(msg, fields)
}

func Error(msg string, fields Field) {
	Default().Error(msg, fields)
}

func Fatal(msg string, fields Field) {
	Default().Fatal(msg, fields)
}

func WithCorrelationID(id string) *CorrelationLogger {
	return Default().WithCorrelationID(id)
}

type AuditLogger struct {
	logger *Logger
}

func NewAuditLogger() *AuditLogger {
	return &AuditLogger{
		logger: Default(),
	}
}

func (a *AuditLogger) Log(action, userID, resource string, success bool, fields Field) {
	if fields == nil {
		fields = Field{}
	}
	fields["action"] = action
	fields["user_id"] = userID
	fields["resource"] = resource
	fields["success"] = success

	a.logger.Info("audit", fields)
}
