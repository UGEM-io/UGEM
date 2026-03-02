// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ugem-io/ugem/logging"
	"github.com/ugem-io/ugem/runtime"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type Server struct {
	UnimplementedGoalRuntimeServer
	runtime    *runtime.GoalRuntime
	listener   net.Listener
	server     *grpc.Server
	mu         sync.RWMutex
	startTime  time.Time
	eventCh    chan *runtime.Event
	shutdownCh chan struct{}
	Version    string
	addr       string
}

type ServerOption func(*Server)

func WithPort(port int) ServerOption {
	return func(s *Server) {
		s.addr = fmt.Sprintf(":%d", port)
	}
}

func NewServer(rt *runtime.GoalRuntime, opts ...ServerOption) *Server {
	s := &Server{
		runtime:    rt,
		startTime:  time.Now(),
		eventCh:    make(chan *runtime.Event, 1000),
		shutdownCh: make(chan struct{}),
		Version:    "1.0.0",
		addr:       ":50051",
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func (s *Server) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.listener = lis
	s.server = grpc.NewServer()
	RegisterGoalRuntimeServer(s.server, s)

	logging.Info("gRPC server starting", logging.Field{
		"addr": s.addr,
	})

	go func() {
		if err := s.server.Serve(lis); err != nil {
			logging.Error("gRPC server error", logging.Field{
				"error": err.Error(),
			})
		}
	}()

	return nil
}

func (s *Server) Stop() error {
	close(s.shutdownCh)
	s.server.GracefulStop()
	logging.Info("gRPC server stopped", logging.Field{})
	return nil
}

func (s *Server) SubmitGoal(ctx context.Context, req *SubmitGoalRequest) (*SubmitGoalResponse, error) {
	logging.Info("SubmitGoal request", logging.Field{
		"name": req.Name,
	})

	goal := runtime.Goal{
		ID:        generateID(),
		Priority:  int(req.Priority),
		Metadata:  req.Metadata,
		State:     runtime.GoalStatePending,
		CreatedAt: time.Now(),
	}

	err := s.runtime.SubmitGoal(goal)
	if err != nil {
		return &SubmitGoalResponse{
			GoalId:  "",
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &SubmitGoalResponse{
		GoalId:  goal.ID,
		Success: true,
	}, nil
}

func (s *Server) GetGoal(ctx context.Context, req *GetGoalRequest) (*GetGoalResponse, error) {
	goal, ok := s.runtime.GetGoalEngine().GetGoal(req.GoalId)
	if !ok {
		return &GetGoalResponse{
			Goal:  nil,
			Error: "goal not found",
		}, nil
	}

	return &GetGoalResponse{
		Goal: goalToProto(&goal),
	}, nil
}

func (s *Server) ListGoals(ctx context.Context, req *ListGoalsRequest) (*ListGoalsResponse, error) {
	goals := s.runtime.GetGoalEngine().ListGoals()

	var protoGoals []*Goal
	for _, g := range goals {
		if req.Status == 0 || goalStateToProto(g.State) == req.Status {
			protoGoals = append(protoGoals, goalToProto(&g))
		}
	}

	return &ListGoalsResponse{
		Goals: protoGoals,
		Total: int32(len(protoGoals)),
	}, nil
}

func (s *Server) CancelGoal(ctx context.Context, req *CancelGoalRequest) (*CancelGoalResponse, error) {
	reason := req.Reason
	if reason == "" {
		reason = "cancelled via gRPC"
	}

	if err := s.runtime.GetGoalEngine().CancelGoal(req.GoalId, reason); err != nil {
		return &CancelGoalResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &CancelGoalResponse{
		Success: true,
	}, nil
}

func (s *Server) StreamEvents(req *StreamEventsRequest, stream GoalRuntime_StreamEventsServer) error {
	logging.Info("Streaming events", logging.Field{
		"goal_id":    req.GoalId,
		"event_type": req.EventType.String(),
	})

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
			length := s.runtime.GetEventLog().Length()
			if length > 0 {
				events, err := s.runtime.GetEventLog().Range(0, length)
				if err == nil {
					for i := range events {
						e := events[i]
						if req.GoalId == "" || fmt.Sprintf("%v", e.Payload["goal_id"]) == req.GoalId {
							if req.EventType == 0 || eventTypeMatches(e.Type, req.EventType) {
								stream.Send(eventToProto(&e))
							}
						}
					}
				}
			}
		case <-s.shutdownCh:
			return nil
		}
	}
}

func (s *Server) GetMetrics(ctx context.Context, req *GetMetricsRequest) (*GetMetricsResponse, error) {
	goals := s.runtime.GetGoalEngine().ListGoals()

	var total, active, completed, failed int64
	for _, g := range goals {
		total++
		switch g.State {
		case runtime.GoalStateActive:
			active++
		case runtime.GoalStateComplete:
			completed++
		case runtime.GoalStateFailed:
			failed++
		}
	}

	return &GetMetricsResponse{
		Metrics: &Metrics{
			TotalGoals:     total,
			ActiveGoals:    active,
			CompletedGoals: completed,
			FailedGoals:    failed,
		},
	}, nil
}

func (s *Server) HealthCheck(ctx context.Context, req *HealthCheckRequest) (*HealthCheckResponse, error) {
	return &HealthCheckResponse{
		Healthy: true,
		Version: s.Version,
		Uptime:  int64(time.Since(s.startTime).Seconds()),
	}, nil
}

func goalToProto(g *runtime.Goal) *Goal {
	var deadline int64
	if !g.Deadline.IsZero() {
		deadline = g.Deadline.Unix()
	}

	return &Goal{
		Id:           g.ID,
		Name:         g.ID,
		Definition:   "",
		Status:       goalStateToProto(g.State),
		Priority:     int32(g.Priority),
		CreatedAt:    g.CreatedAt.Unix(),
		UpdatedAt:    g.CreatedAt.Unix(),
		Deadline:     deadline,
		Metadata:     g.Metadata,
		Dependencies: nil,
		Error:        g.FailReason,
		RetryCount:   int32(g.Attempts),
	}
}

func goalStateToProto(state runtime.GoalState) GoalStatus {
	switch state {
	case runtime.GoalStatePending:
		return GoalStatus_PENDING
	case runtime.GoalStateActive:
		return GoalStatus_RUNNING
	case runtime.GoalStateComplete:
		return GoalStatus_COMPLETED
	case runtime.GoalStateFailed:
		return GoalStatus_FAILED
	case runtime.GoalStateCancelled:
		return GoalStatus_CANCELLED
	default:
		return GoalStatus_PENDING
	}
}

func eventToProto(e *runtime.Event) *Event {
	return &Event{
		Id:        fmt.Sprintf("%d", e.ID),
		GoalId:    "",
		Timestamp: e.Timestamp.Unix(),
		Type:      eventTypeToProto(e.Type),
		Payload:   "",
	}
}

func eventTypeToProto(t string) EventType {
	switch t {
	case "state_change":
		return EventType_STATE_CHANGES
	case "action":
		return EventType_ACTION_EXECUTION
	case "goal":
		return EventType_GOAL_LIFECYCLE
	default:
		return EventType_ALL
	}
}

func eventTypeMatches(eventType string, filter EventType) bool {
	switch filter {
	case EventType_STATE_CHANGES:
		return eventType == "state_change"
	case EventType_ACTION_EXECUTION:
		return eventType == "action"
	case EventType_GOAL_LIFECYCLE:
		return eventType == "goal"
	default:
		return true
	}
}

func generateID() string {
	return fmt.Sprintf("goal-%d", time.Now().UnixNano())
}

type Config struct {
	Port           int
	MaxConns       int
	MaxRecvMsgSize int
	MaxSendMsgSize int
	CertFile       string
	KeyFile        string
	EnableTLS      bool
}

func NewServerWithConfig(rt *runtime.GoalRuntime, cfg *Config) *Server {
	opts := []grpc.ServerOption{}

	if cfg.EnableTLS {
		creds, err := credentials.NewServerTLSFromFile(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			logging.Error("Failed to load TLS credentials", logging.Field{
				"error": err.Error(),
			})
		} else {
			opts = append(opts, grpc.Creds(creds))
		}
	}

	if cfg.MaxRecvMsgSize > 0 {
		opts = append(opts, grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize))
	}

	if cfg.MaxSendMsgSize > 0 {
		opts = append(opts, grpc.MaxSendMsgSize(cfg.MaxSendMsgSize))
	}

	opts = append(opts, grpc.ConnectionTimeout(time.Duration(cfg.MaxConns)*time.Second))

	s := &Server{
		runtime:   rt,
		startTime: time.Now(),
		eventCh:   make(chan *runtime.Event, 1000),
		Version:   "1.0.0",
		addr:      fmt.Sprintf(":%d", cfg.Port),
	}

	s.server = grpc.NewServer(opts...)
	RegisterGoalRuntimeServer(s.server, s)

	return s
}

func Dial(addr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	return conn, nil
}
