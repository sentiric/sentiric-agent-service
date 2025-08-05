package handler

import (
	"context"
	"database/sql"
	"io"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
)

// --- Mock Bağımlılıklar ---
type MockMediaServiceClient struct {
	mock.Mock
}

func (m *MockMediaServiceClient) PlayAudio(ctx context.Context, in *mediav1.PlayAudioRequest, opts ...grpc.CallOption) (*mediav1.PlayAudioResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*mediav1.PlayAudioResponse), args.Error(1)
}
func (m *MockMediaServiceClient) AllocatePort(ctx context.Context, in *mediav1.AllocatePortRequest, opts ...grpc.CallOption) (*mediav1.AllocatePortResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*mediav1.AllocatePortResponse), args.Error(1)
}
func (m *MockMediaServiceClient) ReleasePort(ctx context.Context, in *mediav1.ReleasePortRequest, opts ...grpc.CallOption) (*mediav1.ReleasePortResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*mediav1.ReleasePortResponse), args.Error(1)
}

type MockUserServiceClient struct {
	mock.Mock
}

func (m *MockUserServiceClient) CreateUser(ctx context.Context, in *userv1.CreateUserRequest, opts ...grpc.CallOption) (*userv1.CreateUserResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*userv1.CreateUserResponse), args.Error(1)
}
func (m *MockUserServiceClient) GetUser(ctx context.Context, in *userv1.GetUserRequest, opts ...grpc.CallOption) (*userv1.GetUserResponse, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*userv1.GetUserResponse), args.Error(1)
}

// --- Testler ---
func TestExtractCallerID(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"Standart SIP URI", "sip:+905551234567@1.2.3.4", "+905551234567"},
		{"Artı işaretsiz SIP URI", "sip:905551234567@1.2.3.4", "905551234567"},
		{"Geçersiz format", "not-a-sip-uri", ""},
		{"Boş string", "", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, extractCallerID(tc.input))
		})
	}
}

func TestHandleCallStarted_PlayAnnouncement(t *testing.T) {
	// --- Kurulum (Arrange) ---
	mockMediaClient := new(MockMediaServiceClient)
	mockUserClient := new(MockUserServiceClient)
	var mockDB *sql.DB = nil
	log := zerolog.New(io.Discard)

	processed := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_processed"}, []string{"event_type"})
	failed := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_failed"}, []string{"event_type", "reason"})

	// DÜZELTME: NewEventHandler fonksiyonu artık pointer beklediği için,
	// pointer olan 'processed' ve 'failed' değişkenlerini doğrudan geçiriyoruz.
	handler := NewEventHandler(mockDB, mockMediaClient, mockUserClient, "", "", log, processed, failed)

	jsonPayload := `{
		"eventType": "call.started",
		"callId": "test-call-123",
		"from": "sip:12345@somewhere.com",
		"media": { "caller_rtp_addr": "1.2.3.4:10000", "server_rtp_port": 20000 },
		"dialplan": {
			"dialplan_id": "dp-1",
			"action": {
				"action": "PLAY_ANNOUNCEMENT",
				"action_data": { "data": { "announcement_id": "ANNOUNCE_TEST_WELCOME" } }
			}
		}
	}`
	body := []byte(jsonPayload)
	expectedAudioPath := "audio/tr/system_error.wav"

	mockMediaClient.On("PlayAudio", mock.Anything, mock.MatchedBy(func(req *mediav1.PlayAudioRequest) bool {
		return req.AudioId == expectedAudioPath && req.RtpTargetAddr == "1.2.3.4:10000"
	})).Return(&mediav1.PlayAudioResponse{}, nil)

	// --- Eylem (Act) ---
	handler.HandleRabbitMQMessage(body)

	// --- Doğrulama (Assert) ---
	mock.AssertExpectationsForObjects(t, mockMediaClient)
	assert.Equal(t, float64(1), testutil.ToFloat64(processed.WithLabelValues("call.started")))
}
