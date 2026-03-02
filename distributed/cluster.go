// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package distributed

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/ugem-io/ugem/logging"
	"github.com/ugem-io/ugem/runtime"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type NodeClient interface {
	SyncState(context.Context, *SyncRequest) (*SyncResponse, error)
}

type Node struct {
	ID       string
	Address  string
	IsLeader bool
	Goals    map[string]*runtime.Goal
	mu       sync.RWMutex
	conn     *grpc.ClientConn
	lastSeen time.Time
}

type Cluster struct {
	nodes     map[string]*Node
	localNode *Node
	mu        sync.RWMutex
	runtime   *runtime.GoalRuntime
	leaderID  string
}

type PartitionStrategy int

const (
	PartitionByHash PartitionStrategy = iota
	PartitionByGoalType
	PartitionByPriority
)

func NewCluster(localID, localAddr string, rt *runtime.GoalRuntime) *Cluster {
	localNode := &Node{
		ID:       localID,
		Address:  localAddr,
		IsLeader: true,
		Goals:    make(map[string]*runtime.Goal),
		lastSeen: time.Now(),
	}

	return &Cluster{
		nodes:     map[string]*Node{localID: localNode},
		localNode: localNode,
		runtime:   rt,
		leaderID:  localID,
	}
}

func (c *Cluster) AddNode(id, addr string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.nodes[id]; exists {
		return fmt.Errorf("node %s already exists", id)
	}

	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to node %s: %w", id, err)
	}

	node := &Node{
		ID:       id,
		Address:  addr,
		conn:     conn,
		lastSeen: time.Now(),
	}

	c.nodes[id] = node
	logging.Info("node added to cluster", logging.Field{"node_id": id, "addr": addr})

	return nil
}

func (c *Cluster) RemoveNode(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if id == c.localNode.ID {
		return fmt.Errorf("cannot remove local node")
	}

	node, exists := c.nodes[id]
	if !exists {
		return fmt.Errorf("node %s not found", id)
	}

	node.conn.Close()
	delete(c.nodes, id)
	logging.Info("node removed from cluster", logging.Field{"node_id": id})

	return nil
}

func (c *Cluster) GetNode(id string) (*Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	node, ok := c.nodes[id]
	return node, ok
}

func (c *Cluster) ListNodes() []*Node {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodes := make([]*Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

func (c *Cluster) GetLeader() *Node {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodes[c.leaderID]
}

func (c *Cluster) ElectLeader() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var newLeader *Node
	var maxGoals int

	for _, node := range c.nodes {
		node.mu.RLock()
		goalCount := len(node.Goals)
		node.mu.RUnlock()

		if goalCount > maxGoals {
			maxGoals = goalCount
			newLeader = node
		}
	}

	if newLeader != nil && newLeader.ID != c.leaderID {
		c.leaderID = newLeader.ID
		newLeader.IsLeader = true

		for id, node := range c.nodes {
			if id != newLeader.ID {
				node.IsLeader = false
			}
		}

		logging.Info("leader elected", logging.Field{"leader_id": newLeader.ID})
	}

	return nil
}

func (c *Cluster) RouteGoal(goalID string, strategy PartitionStrategy) (*Node, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.nodes) == 0 {
		return nil, fmt.Errorf("no nodes in cluster")
	}

	switch strategy {
	case PartitionByHash:
		h := fnv.New64a()
		h.Write([]byte(goalID))
		index := int(h.Sum64()) % len(c.nodes)
		i := 0
		for _, node := range c.nodes {
			if i == index {
				return node, nil
			}
			i++
		}
	case PartitionByGoalType:
		goal, ok := c.localNode.Goals[goalID]
		if !ok {
			return c.localNode, nil
		}
		goalType := fmt.Sprintf("%T", goal)
		h := fnv.New64a()
		h.Write([]byte(goalType))
		index := int(h.Sum64()) % len(c.nodes)
		i := 0
		for _, node := range c.nodes {
			if i == index {
				return node, nil
			}
			i++
		}
	case PartitionByPriority:
		goal, ok := c.localNode.Goals[goalID]
		if !ok {
			return c.localNode, nil
		}
		priority := goal.Priority
		index := priority % len(c.nodes)
		i := 0
		for _, node := range c.nodes {
			if i == index {
				return node, nil
			}
			i++
		}
	}

	return c.nodes[c.leaderID], nil
}

func (c *Cluster) DistributeGoal(goal *runtime.Goal) (*Node, error) {
	node, err := c.RouteGoal(goal.ID, PartitionByHash)
	if err != nil {
		return nil, err
	}

	node.mu.Lock()
	node.Goals[goal.ID] = goal
	node.mu.Unlock()

	logging.Info("goal distributed", logging.Field{
		"goal_id": goal.ID,
		"node_id": node.ID,
	})

	return node, nil
}

func (c *Cluster) ReplicateGoal(goal *runtime.Goal, replicationFactor int) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodeList := make([]*Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodeList = append(nodeList, node)
	}

	h := fnv.New64a()
	h.Write([]byte(goal.ID))
	startIndex := int(h.Sum64()) % len(nodeList)

	for i := 0; i < replicationFactor && i < len(nodeList); i++ {
		nodeIndex := (startIndex + i) % len(nodeList)
		node := nodeList[nodeIndex]

		node.mu.Lock()
		node.Goals[goal.ID] = goal
		node.mu.Unlock()

		logging.Info("goal replicated", logging.Field{
			"goal_id": goal.ID,
			"node_id": node.ID,
			"replica": i + 1,
		})
	}

	return nil
}

func (c *Cluster) SyncState() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snap, err := c.runtime.GetStateSnapshot()
	if err != nil {
		return fmt.Errorf("failed to get state snapshot: %w", err)
	}

	state := snap.State()
	stateMap := make(map[string]interface{})
	for path, value := range state {
		stateMap[string(path)] = value.Value
	}

	for _, node := range c.nodes {
		if node.ID == c.localNode.ID {
			continue
		}

		logging.Info("syncing state to node", logging.Field{
			"node_id":    node.ID,
			"state_keys": len(stateMap),
		})
	}

	return nil
}

func (c *Cluster) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for id, node := range c.nodes {
		if node.conn != nil {
			node.conn.Close()
		}
		delete(c.nodes, id)
	}

	logging.Info("cluster shut down", logging.Field{})
}

type SyncRequest struct {
	State  map[string]interface{}
	Clock  int64
	NodeID string
}

type SyncResponse struct {
	Success bool
	Error   string
}

func (c *Cluster) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var totalGoals int
	leaderCount := 0

	for _, node := range c.nodes {
		node.mu.RLock()
		totalGoals += len(node.Goals)
		node.mu.RUnlock()

		if node.IsLeader {
			leaderCount++
		}
	}

	return map[string]interface{}{
		"total_nodes":  len(c.nodes),
		"leader_id":    c.leaderID,
		"total_goals":  totalGoals,
		"leader_count": leaderCount,
	}
}
