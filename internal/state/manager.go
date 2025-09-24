// sentiric-agent-service/internal/state/manager.go
package state

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
)

// MatchedContactPayload, dialplan çözümlemesinden dönen contact verisini temsil eder.
type MatchedContactPayload struct {
	ID           int32  `json:"id"`
	UserID       string `json:"userId"`
	ContactType  string `json:"contactType"`
	ContactValue string `json:"contactValue"`
	IsPrimary    bool   `json:"isPrimary"`
}

// MatchedUserPayload, dialplan çözümlemesinden dönen user verisini temsil eder.
type MatchedUserPayload struct {
	ID                    string                   `json:"id"`
	Name                  *string                  `json:"name"`
	TenantID              string                   `json:"tenantId"`
	UserType              string                   `json:"userType"`
	Contacts              []*MatchedContactPayload `json:"contacts"`
	PreferredLanguageCode *string                  `json:"preferredLanguageCode"`
}

// ActionDataPayload, dialplan'deki eylem verisini temsil eder.
type ActionDataPayload struct {
	Data map[string]string `json:"data"`
}

// DialplanActionPayload, dialplan'deki eylemi temsil eder.
type DialplanActionPayload struct {
	Action     string             `json:"action"`
	ActionData *ActionDataPayload `json:"actionData"`
}

// DialplanPayload, call.started olayının içindeki zenginleştirilmiş dialplan verisini temsil eder.
// --- DÜZELTME BURADA: Gelen JSON'daki alan adlarıyla eşleşen json etiketleri eklendi ---
type DialplanPayload struct {
	DialplanID     string                 `json:"dialplanId"`
	TenantID       string                 `json:"tenantId"`
	Action         *DialplanActionPayload `json:"action"`
	MatchedUser    *MatchedUserPayload    `json:"matchedUser"`
	MatchedContact *MatchedContactPayload `json:"matchedContact"`
	InboundRoute   struct {
		DefaultLanguageCode string `json:"defaultLanguageCode"`
	} `json:"inboundRoute"`
}

// CallEvent, RabbitMQ'dan gelen call.started olayının yapısını temsil eder.
// --- DÜZELTME BURADA: Gelen JSON'daki alan adlarıyla eşleşen json etiketleri eklendi ---
type CallEvent struct {
	EventType string          `json:"eventType"`
	TraceID   string          `json:"traceId"`
	CallID    string          `json:"callId"`
	Media     map[string]interface{} `json:"mediaInfo"` // Düzeltme: mediaInfo
	From      string          `json:"fromUri"`       // Düzeltme: fromUri
	Dialplan  *DialplanPayload `json:"dialplanResolution"` // Düzeltme: dialplanResolution
}
// --- DÜZELTMELERİN SONU ---


// CallState, bir çağrının yaşam döngüsü boyunca Redis'te saklanan durumunu temsil eder.
type CallState struct {
	CallID              string
	TraceID             string
	TenantID            string
	CurrentState        constants.DialogState
	Event               *CallEvent
	Conversation        []map[string]string
	ConsecutiveFailures int
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
	val, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return m.rdb.Set(ctx, key, val, 2*time.Hour).Err()
}