// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrActionTimeout      = errors.New("action timeout")
	ErrActionFailed       = errors.New("action failed")
	ErrNoAvailableWorkers = errors.New("no available workers")
	ErrInvalidAction      = errors.New("invalid action")
	ErrActionCancelled    = errors.New("action cancelled")
	ErrBackoffExhausted   = errors.New("backoff exhausted")
)

type worker struct {
	id     int
	taskCh chan *task
	stopCh chan struct{}
}

type task struct {
	action      Action
	ctx         ActionContext
	resultCh    chan ActionResult
	startTime   time.Time
	cancelCh    chan struct{}
	backoffTime time.Duration
}

type ActionDispatcherImpl struct {
	mu           sync.RWMutex
	workers      []*worker
	workerCount  int
	taskQueue    chan *task
	resultCache  map[string]ActionResult
	activeTasks  map[string]*task
	clock        LogicalClock
	stopCh       chan struct{}
	wg           sync.WaitGroup
	running      bool
	shortTermMem *ShortTermMemory
	metrics      *Metrics
	externalWorkers map[string]string
	planner         Planner
}

func NewActionDispatcher(workerCount int) *ActionDispatcherImpl {
	ad := &ActionDispatcherImpl{
		workerCount:  workerCount,
		taskQueue:    make(chan *task, 1000),
		resultCache:  make(map[string]ActionResult),
		activeTasks:  make(map[string]*task),
		stopCh:       make(chan struct{}),
		running:      false,
		shortTermMem:    NewShortTermMemory(1000),
		metrics:         NewMetrics(),
		externalWorkers: make(map[string]string),
	}

	return ad
}

func (ad *ActionDispatcherImpl) Execute(action Action, ctx ActionContext) (map[string]interface{}, error) {
	var cancelFunc context.CancelFunc
	ctx.CancelCtx, cancelFunc = context.WithCancel(context.Background())
	ctx.CancelFunc = cancelFunc
	defer cancelFunc()

	return ad.executeWithRetry(action, ctx)
}

func (ad *ActionDispatcherImpl) executeWithRetry(action Action, ctx ActionContext) (map[string]interface{}, error) {
	timeout := action.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	maxRetries := action.Retries
	if maxRetries == 0 {
		maxRetries = 1
	}

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		ctx.RetryCount = attempt

		select {
		case <-ctx.CancelCtx.Done():
			return nil, ErrActionCancelled
		default:
		}

		startTime := time.Now()
		result, err := ad.executeOnce(action, ctx, timeout)
		elapsed := time.Since(startTime)

		if err == nil {
			ad.metrics.RecordActionSuccess(elapsed)
			ad.shortTermMem.AddRecord(ExecutionRecord{
				ID:           action.ID,
				GoalID:       fmt.Sprintf("%d", ctx.EventID),
				ActionID:     action.ID,
				StartTime:    startTime,
				EndTime:      time.Now(),
				Status:       "success",
				Input:        action.Input,
				Output:       result,
				RetryAttempt: attempt,
			})
			return result, nil
		}

		lastErr = err
		ad.metrics.RecordActionFailure(elapsed)
		ad.shortTermMem.AddRecord(ExecutionRecord{
			ID:           action.ID,
			GoalID:       fmt.Sprintf("%d", ctx.EventID),
			ActionID:     action.ID,
			StartTime:    startTime,
			EndTime:      time.Now(),
			Status:       "failed",
			Input:        action.Input,
			Error:        err,
			RetryAttempt: attempt,
		})
		ad.shortTermMem.RecordFailure(fmt.Sprintf("%d", ctx.EventID), action.ID, err.Error())

		if attempt < maxRetries-1 {
			backoffDuration := ad.calculateBackoff(action.Backoff, attempt)
			select {
			case <-time.After(backoffDuration):
			case <-ctx.CancelCtx.Done():
				return nil, ErrActionCancelled
			}
		}
	}

	return nil, lastErr
}

func (ad *ActionDispatcherImpl) calculateBackoff(backoff *RetryStrategy, attempt int) time.Duration {
	if backoff == nil {
		return time.Duration(attempt+1) * 100 * time.Millisecond
	}

	delay := backoff.InitialDelay
	if delay == 0 {
		delay = 100 * time.Millisecond
	}

	maxDelay := backoff.MaxDelay
	if maxDelay == 0 {
		maxDelay = 30 * time.Second
	}

	switch backoff.Strategy {
	case BackoffLinear:
		delay = delay * time.Duration(attempt+1)
	case BackoffExponential:
		for i := 0; i < attempt; i++ {
			delay = delay * 2
		}
	case BackoffFibonacci:
		a, b := 1, 1
		for i := 0; i < attempt; i++ {
			a, b = b, a+b
		}
		delay = delay * time.Duration(a)
	}

	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

func (ad *ActionDispatcherImpl) executeOnce(action Action, ctx ActionContext, timeout time.Duration) (map[string]interface{}, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx.CancelCtx, timeout)
	defer cancel()

	resultCh := make(chan ActionResult, 1)
	cancelCh := make(chan struct{}, 1)

	task := &task{
		action:    action,
		ctx:       ctx,
		resultCh:  resultCh,
		startTime: time.Now(),
		cancelCh:  cancelCh,
	}

	select {
	case ad.taskQueue <- task:
	case <-ctxWithTimeout.Done():
		return nil, ErrActionTimeout
	}

	select {
	case result := <-resultCh:
		if !result.Success {
			return nil, result.Error
		}
		return result.Output, nil
	case <-ctxWithTimeout.Done():
		return nil, ErrActionTimeout
	case <-cancelCh:
		return nil, ErrActionCancelled
	}
}

func (ad *ActionDispatcherImpl) ExecuteAsync(action Action, ctx ActionContext) (<-chan ActionResult, error) {
	resultCh := make(chan ActionResult, 1)

	task := &task{
		action:    action,
		ctx:       ctx,
		resultCh:  resultCh,
		startTime: time.Now(),
	}

	select {
	case ad.taskQueue <- task:
		return resultCh, nil
	default:
		return nil, ErrNoAvailableWorkers
	}
}

func (ad *ActionDispatcherImpl) CancelAction(actionID string) bool {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	if t, exists := ad.activeTasks[actionID]; exists {
		if t.cancelCh != nil {
			select {
			case t.cancelCh <- struct{}{}:
			default:
			}
		}
		delete(ad.activeTasks, actionID)
		return true
	}
	return false
}

func (ad *ActionDispatcherImpl) Start() error {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	if ad.running {
		return nil
	}

	ad.running = true
	ad.stopCh = make(chan struct{})
	ad.workers = make([]*worker, ad.workerCount)

	for i := 0; i < ad.workerCount; i++ {
		worker := &worker{
			id:     i,
			taskCh: make(chan *task),
			stopCh: make(chan struct{}),
		}
		ad.workers[i] = worker

		ad.wg.Add(1)
		go ad.runWorker(worker)
	}

	return nil
}

func (ad *ActionDispatcherImpl) Stop() error {
	ad.mu.Lock()

	if !ad.running {
		ad.mu.Unlock()
		return nil
	}

	if ad.stopCh != nil {
		close(ad.stopCh)
	}

	for _, worker := range ad.workers {
		if worker.stopCh != nil {
			close(worker.stopCh)
		}
	}

	ad.mu.Unlock()

	ad.wg.Wait()

	ad.mu.Lock()
	ad.running = false
	ad.stopCh = nil
	ad.workers = nil
	ad.mu.Unlock()

	return nil
}

func (ad *ActionDispatcherImpl) runWorker(w *worker) {
	defer ad.wg.Done()

	for {
		select {
		case <-w.stopCh:
			return
		case task := <-ad.taskQueue:
			ad.executeTask(w, task)
		}
	}
}

func (ad *ActionDispatcherImpl) executeTask(w *worker, task *task) {
	var result ActionResult

	handler := task.action.Execute
	if handler == nil && ad.planner != nil {
		if resolver, ok := ad.planner.GetActionResolver(task.action.Type); ok {
			handler = func(ctx ActionContext) (map[string]interface{}, error) {
				return resolver.Handler(task.action.Input, ctx)
			}
		}
	}

	if handler != nil {
		output, err := handler(task.ctx)
		if err != nil {
			result = ActionResult{
				ActionID: task.action.ID,
				Success:  false,
				Error:    err,
			}
		} else {
			result = ActionResult{
				ActionID: task.action.ID,
				Success:  true,
				Output:   output,
			}
		}
	} else if addr, exists := ad.externalWorkers[task.action.Type]; exists {
		output, err := ad.dispatchToExternalWorker(addr, task.action, task.ctx)
		if err != nil {
			result = ActionResult{
				ActionID: task.action.ID,
				Success:  false,
				Error:    err,
			}
		} else {
			result = ActionResult{
				ActionID: task.action.ID,
				Success:  true,
				Output:   output,
			}
		}
	} else {
		result = ActionResult{
			ActionID: task.action.ID,
			Success:  false,
			Error:    ErrInvalidAction,
		}
	}

	ad.mu.Lock()
	ad.activeTasks[task.action.ID] = task
	ad.resultCache[task.action.ID] = result
	delete(ad.activeTasks, task.action.ID)
	ad.mu.Unlock()

	select {
	case task.resultCh <- result:
	default:
	}
}

func (ad *ActionDispatcherImpl) WorkerCount() int {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.workerCount
}

func (ad *ActionDispatcherImpl) SetWorkerCount(count int) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	if ad.running {
		return
	}

	ad.workerCount = count
}

func (ad *ActionDispatcherImpl) GetResult(actionID string) (ActionResult, bool) {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	result, exists := ad.resultCache[actionID]
	return result, exists
}

func (ad *ActionDispatcherImpl) ClearCache() {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.resultCache = make(map[string]ActionResult)
}

func (ad *ActionDispatcherImpl) SetPlanner(p Planner) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.planner = p
}

func (ad *ActionDispatcherImpl) IsRunning() bool {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.running
}

func (ad *ActionDispatcherImpl) GetClock() LogicalClock {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.clock
}

func (ad *ActionDispatcherImpl) AdvanceClock() LogicalClock {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.clock++
	return ad.clock
}

func (ad *ActionDispatcherImpl) PendingTasks() int {
	return len(ad.taskQueue)
}

func (ad *ActionDispatcherImpl) GetShortTermMemory() *ShortTermMemory {
	return ad.shortTermMem
}

func (ad *ActionDispatcherImpl) GetMetrics() *Metrics {
	return ad.metrics
}

func (ad *ActionDispatcherImpl) RegisterExternalWorker(actionType string, addr string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.externalWorkers[actionType] = addr
}

func (ad *ActionDispatcherImpl) dispatchToExternalWorker(addr string, action Action, ctx ActionContext) (map[string]interface{}, error) {
	// Idempotency key calculation
	idempotencyKey := fmt.Sprintf("idempotency-%s-%s", ctx.Trace.TraceID, action.ID)

	// Simulate network delay
	time.Sleep(50 * time.Millisecond)

	// Mock response
	return map[string]interface{}{
		"worker_addr":     addr,
		"idempotency_key": idempotencyKey,
		"status":          "completed",
		"trace_id":        ctx.Trace.TraceID,
	}, nil
}
