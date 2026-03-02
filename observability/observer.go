// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package observability

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ugem-io/ugem/logging"
	"github.com/ugem-io/ugem/runtime"
)

type HealthChecker struct {
	checks    map[string]HealthCheck
	mu        sync.RWMutex
	runtime   *runtime.GoalRuntime
	startTime time.Time
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

type HealthCheck struct {
	Name      string
	Status    HealthStatus
	Message   string
	LastCheck time.Time
}

type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

func NewHealthChecker(rt *runtime.GoalRuntime) *HealthChecker {
	hc := &HealthChecker{
		checks:    make(map[string]HealthCheck),
		runtime:   rt,
		startTime: time.Now(),
		stopCh:    make(chan struct{}),
	}

	hc.registerDefaultChecks()
	return hc
}

func (h *HealthChecker) registerDefaultChecks() {
	h.RegisterCheck("runtime", func() HealthCheck {
		if h.runtime.IsRunning() {
			return HealthCheck{
				Name:      "runtime",
				Status:    HealthStatusHealthy,
				Message:   "Runtime is running",
				LastCheck: time.Now(),
			}
		}
		return HealthCheck{
			Name:      "runtime",
			Status:    HealthStatusUnhealthy,
			Message:   "Runtime is not running",
			LastCheck: time.Now(),
		}
	})

	h.RegisterCheck("eventlog", func() HealthCheck {
		length := h.runtime.GetEventLog().Length()
		return HealthCheck{
			Name:      "eventlog",
			Status:    HealthStatusHealthy,
			Message:   fmt.Sprintf("Event log has %d events", length),
			LastCheck: time.Now(),
		}
	})

	h.RegisterCheck("goals", func() HealthCheck {
		goals := h.runtime.GetGoalEngine().ListGoals()
		var active, failed int
		for _, g := range goals {
			switch g.State {
			case runtime.GoalStateActive:
				active++
			case runtime.GoalStateFailed:
				failed++
			}
		}
		if failed > len(goals)/2 {
			return HealthCheck{
				Name:      "goals",
				Status:    HealthStatusDegraded,
				Message:   fmt.Sprintf("%d active, %d failed", active, failed),
				LastCheck: time.Now(),
			}
		}
		return HealthCheck{
			Name:      "goals",
			Status:    HealthStatusHealthy,
			Message:   fmt.Sprintf("%d active, %d failed", active, failed),
			LastCheck: time.Now(),
		}
	})
}

func (h *HealthChecker) RegisterCheck(name string, check func() HealthCheck) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = HealthCheck{Name: name}
	h.runCheck(name, check)
}

func (h *HealthChecker) runCheck(name string, check func() HealthCheck) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		for {
			result := check()
			h.mu.Lock()
			h.checks[name] = result
			h.mu.Unlock()
			select {
			case <-h.stopCh:
				return
			case <-time.After(30 * time.Second):
			}
		}
	}()
}

func (h *HealthChecker) Stop() {
	close(h.stopCh)
	h.wg.Wait()
}

func (h *HealthChecker) GetHealth() (map[string]HealthCheck, HealthStatus) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	results := make(map[string]HealthCheck)
	overallStatus := HealthStatusHealthy

	for name, check := range h.checks {
		results[name] = check
		if check.Status == HealthStatusUnhealthy {
			overallStatus = HealthStatusUnhealthy
		} else if check.Status == HealthStatusDegraded && overallStatus != HealthStatusUnhealthy {
			overallStatus = HealthStatusDegraded
		}
	}

	return results, overallStatus
}

func (h *HealthChecker) GetUptime() time.Duration {
	return time.Since(h.startTime)
}

type MetricsCollector struct {
	runtime        *runtime.GoalRuntime
	requestCount   atomic.Int64
	errorCount     atomic.Int64
	totalLatencyMs atomic.Int64
	mu             sync.RWMutex
	startTime      time.Time
}

func NewMetricsCollector(rt *runtime.GoalRuntime) *MetricsCollector {
	return &MetricsCollector{
		runtime:   rt,
		startTime: time.Now(),
	}
}

func (m *MetricsCollector) RecordRequest(durationMs int64) {
	m.requestCount.Add(1)
	m.totalLatencyMs.Add(durationMs)
}

func (m *MetricsCollector) RecordError() {
	m.errorCount.Add(1)
}

type SystemMetrics struct {
	Requests       int64   `json:"requests"`
	Errors         int64   `json:"errors"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	UptimeSeconds  float64 `json:"uptime_seconds"`
	TotalGoals     int64   `json:"total_goals"`
	ActiveGoals    int64   `json:"active_goals"`
	CompletedGoals int64   `json:"completed_goals"`
	FailedGoals    int64   `json:"failed_goals"`
	PendingGoals   int64   `json:"pending_goals"`
	EventsLogged   int64   `json:"events_logged"`
}

func (m *MetricsCollector) GetMetrics() SystemMetrics {
	goals := m.runtime.GetGoalEngine().ListGoals()
	events := m.runtime.GetEventLog().Length()

	var total, active, completed, failed, pending int64
	for _, g := range goals {
		total++
		switch g.State {
		case runtime.GoalStateActive:
			active++
		case runtime.GoalStateComplete:
			completed++
		case runtime.GoalStateFailed:
			failed++
		case runtime.GoalStatePending:
			pending++
		}
	}

	requests := m.requestCount.Load()
	var avgLatency float64
	if requests > 0 {
		avgLatency = float64(m.totalLatencyMs.Load()) / float64(requests)
	}

	return SystemMetrics{
		Requests:       requests,
		Errors:         m.errorCount.Load(),
		AvgLatencyMs:   avgLatency,
		UptimeSeconds:  time.Since(m.startTime).Seconds(),
		TotalGoals:     total,
		ActiveGoals:    active,
		CompletedGoals: completed,
		FailedGoals:    failed,
		PendingGoals:   pending,
		EventsLogged:   int64(events),
	}
}

type Tracer struct {
	runtime *runtime.GoalRuntime
	spans   map[string]*Span
	mu      sync.RWMutex
	enabled atomic.Bool
}

type Span struct {
	Name      string
	TraceID   string
	SpanID    string
	ParentID  string
	StartTime time.Time
	EndTime   time.Time
	Status    string
	Attrs     map[string]string
}

func NewTracer(rt *runtime.GoalRuntime) *Tracer {
	return &Tracer{
		runtime: rt,
		spans:   make(map[string]*Span),
		enabled: atomic.Bool{},
	}
}

func (t *Tracer) Enable() {
	t.enabled.Store(true)
	logging.Info("Tracer enabled", logging.Field{})
}

func (t *Tracer) Disable() {
	t.enabled.Store(false)
	logging.Info("Tracer disabled", logging.Field{})
}

func (t *Tracer) IsEnabled() bool {
	return t.enabled.Load()
}

func (t *Tracer) StartSpan(name, traceID, parentID string) *Span {
	if !t.enabled.Load() {
		return nil
	}

	span := &Span{
		Name:      name,
		TraceID:   traceID,
		SpanID:    fmt.Sprintf("span-%d", time.Now().UnixNano()),
		ParentID:  parentID,
		StartTime: time.Now(),
		Attrs:     make(map[string]string),
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.spans[span.SpanID] = span

	return span
}

func (t *Tracer) EndSpan(span *Span) {
	if span == nil || !t.enabled.Load() {
		return
	}

	span.EndTime = time.Now()
	span.Status = "ok"

	logging.Info("span completed", logging.Field{
		"name":        span.Name,
		"trace_id":    span.TraceID,
		"span_id":     span.SpanID,
		"duration_ms": time.Since(span.StartTime).Milliseconds(),
	})
}

func (t *Tracer) AddSpanAttr(span *Span, key, value string) {
	if span == nil {
		return
	}
	span.Attrs[key] = value
}

func (t *Tracer) GetSpans(traceID string) []*Span {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*Span
	for _, span := range t.spans {
		if span.TraceID == traceID {
			result = append(result, span)
		}
	}
	return result
}

func (t *Tracer) ClearSpans() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.spans = make(map[string]*Span)
}

func StartTrace(ctx context.Context, tracer *Tracer, name string) (context.Context, *Span) {
	if tracer == nil || !tracer.IsEnabled() {
		return ctx, nil
	}

	traceID := fmt.Sprintf("trace-%d", time.Now().UnixNano())
	span := tracer.StartSpan(name, traceID, "")
	return context.WithValue(ctx, "span", span), span
}

func EndTrace(tracer *Tracer, span *Span) {
	if tracer != nil && span != nil {
		tracer.EndSpan(span)
	}
}

type Observer struct {
	HealthChecker    *HealthChecker
	MetricsCollector *MetricsCollector
	Tracer           *Tracer
}

func NewObserver(rt *runtime.GoalRuntime) *Observer {
	return &Observer{
		HealthChecker:    NewHealthChecker(rt),
		MetricsCollector: NewMetricsCollector(rt),
		Tracer:           NewTracer(rt),
	}
}

func (o *Observer) GetHealth() (map[string]HealthCheck, HealthStatus) {
	return o.HealthChecker.GetHealth()
}

func (o *Observer) GetMetrics() SystemMetrics {
	return o.MetricsCollector.GetMetrics()
}

func (o *Observer) EnableTracing() {
	o.Tracer.Enable()
}

func (o *Observer) DisableTracing() {
	o.Tracer.Disable()
}

func (o *Observer) Stop() {
	o.HealthChecker.Stop()
}
