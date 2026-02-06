// sentiric-agent-service/internal/state/manager.go
package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
)

const SessionTTL = 2 * time.Hour

// MediaInfoPayload, olaylardaki medya verilerini temsil eder.
type MediaInfoPayload struct {
	CallerRtpAddr string  `json:"callerRtpAddr"`
	ServerRtpPort float64 `json:"serverRtpPort"`
}

// CallEvent, ham olay verilerini temsil eder.
type CallEvent struct {
	EventType string            `json:"eventType"`
	TraceID   string            `json:"traceId"`
	CallID    string            `json:"callId"`
	Media     *MediaInfoPayload `json:"mediaInfo"`
	From      string            `json:"fromUri"`
	To        string            `json:"toUri"`
}

// CallState, Redis'te saklanan orkestrasyon durumudur.
type CallState struct {
	CallID              string                `json:"callId"`
	TraceID             string                `json:"traceId"`
	TenantID            string                `json:"tenantId"`
	CurrentState        constants.DialogState `json:"currentState"`
	FromURI             string                `json:"fromUri"`
	ToURI               string                `json:"toUri"`
	ServerRtpPort       uint32                `json:"serverRtpPort"`
	CallerRtpAddr       string                `json:"callerRtpAddr"`
	ConsecutiveFailures int                   `json:"consecutiveFailures"`
	CreatedAt           time.Time             `json:"createdAt"`
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
	key := "callstate:" + callID
	val, err := m.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get error: %w", err)
	}
	var state CallState
	if err := json.Unmarshal([]byte(val), &state); err != nil {
		return nil, fmt.Errorf("json unmarshal error: %w", err)
	}
	return &state, nil
}

func (m *Manager) Set(ctx context.Context, state *CallState) error {
	key := "callstate:" + state.CallID
	val, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return m.rdb.Set(ctx, key, val, SessionTTL).Err()
}

func (m *Manager) Delete(ctx context.Context, callID string) error {
	return m.rdb.Del(ctx, "callstate:"+callID).Err()
}
