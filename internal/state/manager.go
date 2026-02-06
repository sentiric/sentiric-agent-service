// sentiric-agent-service/internal/state/manager.go
package state

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
)

const SessionTTL = 2 * time.Hour

// CallState, Redis'te saklanan aktif orkestrasyon verisidir.
type CallState struct {
	CallID         string                `json:"callId"`
	TraceID        string                `json:"traceId"`
	TenantID       string                `json:"tenantId"`
	CurrentState   constants.DialogState `json:"currentState"`
	FromURI        string                `json:"fromUri"`
	ToURI          string                `json:"toUri"`
	ServerRtpPort  uint32                `json:"serverRtpPort"`
	CallerRtpAddr  string                `json:"callerRtpAddr"`
	CreatedAt      time.Time             `json:"createdAt"`
	PipelineActive bool                  `json:"pipelineActive"`
}

type Manager struct {
	rdb *redis.Client
}

func NewManager(rdb *redis.Client) *Manager {
	return &Manager{rdb: rdb}
}

func (m *Manager) RedisClient() *redis.Client {
	return m.rdb
}

func (m *Manager) Get(ctx context.Context, callID string) (*CallState, error) {
	val, err := m.rdb.Get(ctx, "callstate:"+callID).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state CallState
	if err := json.Unmarshal([]byte(val), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Manager) Set(ctx context.Context, state *CallState) error {
	val, _ := json.Marshal(state)
	return m.rdb.Set(ctx, "callstate:"+state.CallID, val, SessionTTL).Err()
}

func (m *Manager) Delete(ctx context.Context, callID string) error {
	return m.rdb.Del(ctx, "callstate:"+callID).Err()
}
