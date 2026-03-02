// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ugem-io/ugem/logging"
	"github.com/ugem-io/ugem/runtime"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	ggrpc "github.com/ugem-io/ugem/grpc"

	"github.com/gorilla/mux"
)

type Server struct {
	runtime   *runtime.GoalRuntime
	grpcConn  *grpc.ClientConn
	listener  net.Listener
	server    *http.Server
	mu        sync.RWMutex
	startTime time.Time
	Version   string
	addr      string
	mux       *mux.Router
}

type ServerOption func(*Server)

func WithHTTPAddr(addr string) ServerOption {
	return func(s *Server) {
		s.addr = addr
	}
}

func NewServer(rt *runtime.GoalRuntime, opts ...ServerOption) *Server {
	s := &Server{
		runtime:   rt,
		startTime: time.Now(),
		Version:   "1.0.0",
		addr:      ":8080",
		mux:       mux.NewRouter(),
	}

	for _, opt := range opts {
		opt(s)
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.mux.Use(LoggingMiddleware)
	s.mux.Use(RecoveryMiddleware)

	s.mux.HandleFunc("/health", s.handleHealth).Methods("GET")
	s.mux.HandleFunc("/metrics", s.handleMetrics).Methods("GET")

	s.mux.HandleFunc("/api/v1/goals", s.handleListGoals).Methods("GET")
	s.mux.HandleFunc("/api/v1/goals", s.handleCreateGoal).Methods("POST")
	s.mux.HandleFunc("/api/v1/goals/{id}", s.handleGetGoal).Methods("GET")
	s.mux.HandleFunc("/api/v1/goals/{id}", s.handleCancelGoal).Methods("DELETE")
	s.mux.HandleFunc("/api/v1/goals/{id}/events", s.handleStreamEvents).Methods("GET")

	s.mux.HandleFunc("/api/v1/state", s.handleGetState).Methods("GET")
	s.mux.HandleFunc("/api/v1/state", s.handleSetState).Methods("POST")

	s.mux.HandleFunc("/api/v1/events", s.handleListEvents).Methods("GET")
}

func (s *Server) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.listener = lis
	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      s.mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logging.Info("HTTP server starting", logging.Field{
		"addr": s.addr,
	})

	go func() {
		if err := s.server.Serve(lis); err != nil && err != http.ErrServerClosed {
			logging.Error("HTTP server error", logging.Field{
				"error": err.Error(),
			})
		}
	}()

	return nil
}

func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logging.Info("HTTP server stopping", logging.Field{})
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"healthy": true,
		"version": s.Version,
		"uptime":  time.Since(s.startTime).Seconds(),
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	goals := s.runtime.GetGoalEngine().ListGoals()

	var total, active, completed, failed, pending int
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

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_goals":     total,
		"active_goals":    active,
		"completed_goals": completed,
		"failed_goals":    failed,
		"pending_goals":   pending,
	})
}

type CreateGoalRequest struct {
	Name       string            `json:"name"`
	Definition string            `json:"definition"`
	Priority   int               `json:"priority"`
	Metadata   map[string]string `json:"metadata"`
}

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request) {
	var req CreateGoalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	goal := runtime.Goal{
		ID:        fmt.Sprintf("goal-%d", time.Now().UnixNano()),
		Priority:  req.Priority,
		Metadata:  req.Metadata,
		State:     runtime.GoalStatePending,
		CreatedAt: time.Now(),
	}

	if err := s.runtime.SubmitGoal(goal); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"goal_id": goal.ID,
		"success": true,
	})
}

func (s *Server) handleListGoals(w http.ResponseWriter, r *http.Request) {
	goals := s.runtime.GetGoalEngine().ListGoals()

	var goalList []map[string]interface{}
	for _, g := range goals {
		goalList = append(goalList, map[string]interface{}{
			"id":          g.ID,
			"priority":    g.Priority,
			"state":       g.State,
			"metadata":    g.Metadata,
			"created_at":  g.CreatedAt.Unix(),
			"fail_reason": g.FailReason,
		})
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"goals": goalList,
		"total": len(goalList),
	})
}

func (s *Server) handleGetGoal(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	goal, ok := s.runtime.GetGoalEngine().GetGoal(id)
	if !ok {
		s.writeError(w, http.StatusNotFound, "goal not found")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":          goal.ID,
		"priority":    goal.Priority,
		"state":       goal.State,
		"metadata":    goal.Metadata,
		"created_at":  goal.CreatedAt.Unix(),
		"started_at":  goal.StartedAt,
		"fail_reason": goal.FailReason,
		"attempts":    goal.Attempts,
	})
}

func (s *Server) handleCancelGoal(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := s.runtime.GetGoalEngine().CancelGoal(id, "cancelled via HTTP"); err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

func (s *Server) handleGetState(w http.ResponseWriter, r *http.Request) {
	snap, err := s.runtime.GetState().Snapshot()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	state := snap.State()
	stateMap := make(map[string]interface{})
	for path, value := range state {
		stateMap[string(path)] = value.Value
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"state": stateMap,
		"clock": snap.Clock(),
	})
}

func (s *Server) handleSetState(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Route through SubmitEvent to maintain event sourcing integrity
	writePaths := make([]runtime.Path, 0, len(req))
	for path := range req {
		writePaths = append(writePaths, runtime.Path(path))
	}

	event := runtime.Event{
		Type:       "state.set",
		WritePaths: writePaths,
		Payload:    req,
		StateMutator: func(state runtime.State) error {
			for path, value := range req {
				state.Set(runtime.Path(path), runtime.TypedValue{
					Type:  "unknown",
					Value: value,
				})
			}
			return nil
		},
	}

	if err := s.runtime.SubmitEvent(event); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	length := s.runtime.GetEventLog().Length()
	events, err := s.runtime.GetEventLog().Range(0, length)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var eventList []map[string]interface{}
	for _, e := range events {
		eventList = append(eventList, map[string]interface{}{
			"id":        e.ID,
			"timestamp": e.Timestamp.Unix(),
			"type":      e.Type,
			"payload":   e.Payload,
		})
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": eventList,
		"total":  len(eventList),
	})
}

func (s *Server) handleStreamEvents(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	goalID := vars["id"]

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	var lastID runtime.EventID
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			length := s.runtime.GetEventLog().Length()
			if length > lastID {
				events, err := s.runtime.GetEventLog().Range(lastID, length)
				if err == nil {
					for _, e := range events {
						if goalID == "" || fmt.Sprintf("%v", e.Payload["goal_id"]) == goalID {
							data, _ := json.Marshal(map[string]interface{}{
								"id":        e.ID,
								"timestamp": e.Timestamp.Unix(),
								"type":      e.Type,
								"payload":   e.Payload,
							})
							fmt.Fprintf(w, "data: %s\n\n", data)
							flusher.Flush()
						}
					}
					lastID = length
				}
			}
		}
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]interface{}{
		"error": message,
	})
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		logging.Info("HTTP request", logging.Field{
			"method": r.Method,
			"path":   r.URL.Path,
			"remote": r.RemoteAddr,
		})
		next.ServeHTTP(w, r)
		logging.Info("HTTP request complete", logging.Field{
			"method":      r.Method,
			"path":        r.URL.Path,
			"duration_ms": time.Since(start).Milliseconds(),
		})
	})
}

func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logging.Error("Panic recovered", logging.Field{
					"error": fmt.Sprintf("%v", err),
				})
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func StartWithGracefulShutdown(rt *runtime.GoalRuntime, httpAddr string, grpcAddr string) error {
	httpServer := NewServer(rt, WithHTTPAddr(httpAddr))

	if err := httpServer.Start(); err != nil {
		return err
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logging.Info("Shutting down servers", logging.Field{})

	if err := httpServer.Stop(); err != nil {
		return err
	}

	return nil
}

type GRPCClient struct {
	conn   *grpc.ClientConn
	client ggrpc.GoalRuntimeClient
}

func NewGRPCClient(addr string) (*GRPCClient, error) {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &GRPCClient{
		conn:   conn,
		client: ggrpc.NewGoalRuntimeClient(conn),
	}, nil
}

func (c *GRPCClient) SubmitGoal(name, definition string, priority int, metadata map[string]string) (string, error) {
	resp, err := c.client.SubmitGoal(context.Background(), &ggrpc.SubmitGoalRequest{
		Name:       name,
		Definition: definition,
		Priority:   int32(priority),
		Metadata:   metadata,
	})
	if err != nil {
		return "", err
	}
	return resp.GoalId, nil
}

func (c *GRPCClient) GetGoal(id string) (*ggrpc.Goal, error) {
	resp, err := c.client.GetGoal(context.Background(), &ggrpc.GetGoalRequest{GoalId: id})
	if err != nil {
		return nil, err
	}
	return resp.Goal, nil
}

func (c *GRPCClient) ListGoals() ([]*ggrpc.Goal, error) {
	resp, err := c.client.ListGoals(context.Background(), &ggrpc.ListGoalsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Goals, nil
}

func (c *GRPCClient) CancelGoal(id, reason string) error {
	_, err := c.client.CancelGoal(context.Background(), &ggrpc.CancelGoalRequest{
		GoalId: id,
		Reason: reason,
	})
	return err
}

func (c *GRPCClient) GetMetrics() (*ggrpc.Metrics, error) {
	resp, err := c.client.GetMetrics(context.Background(), &ggrpc.GetMetricsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Metrics, nil
}

func (c *GRPCClient) HealthCheck() (*ggrpc.HealthCheckResponse, error) {
	return c.client.HealthCheck(context.Background(), &ggrpc.HealthCheckRequest{})
}

func (c *GRPCClient) StreamEvents(goalID string) (ggrpc.GoalRuntime_StreamEventsClient, error) {
	return c.client.StreamEvents(context.Background(), &ggrpc.StreamEventsRequest{
		GoalId: goalID,
	})
}

func (c *GRPCClient) Close() error {
	return c.conn.Close()
}
