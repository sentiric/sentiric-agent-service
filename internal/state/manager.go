// File: internal/state/manager.go
package state

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
)

type DialogState string

const (
	StateWelcoming  DialogState = "WELCOMING"
	StateListening  DialogState = "LISTENING"
	StateThinking   DialogState = "THINKING"
	StateSpeaking   DialogState = "SPEAKING"
	StateEnded      DialogState = "ENDED"
	StateTerminated DialogState = "TERMINATED"
)

type CallEvent struct {
	EventType string                              `json:"eventType"`
	TraceID   string                              `json:"traceId"`
	CallID    string                              `json:"callId"`
	Media     map[string]interface{}              `json:"media"`
	Dialplan  *dialplanv1.ResolveDialplanResponse `json:"dialplan"`
	From      string                              `json:"from"`
}

type CallState struct {
	CallID       string
	TraceID      string
	TenantID     string
	CurrentState DialogState
	Event        *CallEvent
	Conversation []map[string]string
}

type Manager struct {
	rdb *redis.Client
}

func NewManager(rdb *redis.Client) *Manager {
	return &Manager{rdb: rdb}
}

func (m *Manager) Get(ctx context.Context, callID string) (*CallState, error) {
	key := "callstate:" + callID
	val, err := m.rdb.Get(ctx, key).Result()
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
	key := "callstate:" + state.CallID
	val, err := json.Marshal(state) // DÜZELTME: val artık kullanılıyor
	if err != nil {
		return err
	}
	// DÜZELTME: Eksik olan 'val' argümanı eklendi.
	return m.rdb.Set(ctx, key, val, 2*time.Hour).Err()
}
