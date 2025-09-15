package service

import (
	"context"
	"strings"
	"time"

	"github.com/sentiric/sentiric-agent-service/internal/ctxlogger"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UserManager, user-service ile olan tüm etkileşimleri yönetir.
type UserManager struct {
	userClient userv1.UserServiceClient
}

// NewUserManager, yeni bir UserManager örneği oluşturur.
func NewUserManager(uc userv1.UserServiceClient) *UserManager {
	return &UserManager{userClient: uc}
}

// FindOrCreateGuest, verilen çağrı olayına göre bir kullanıcıyı bulur veya oluşturur.
func (um *UserManager) FindOrCreateGuest(ctx context.Context, event *state.CallEvent) (*userv1.User, *userv1.Contact, error) {
	l := ctxlogger.FromContext(ctx)
	callerNumber := event.From
	if strings.Contains(callerNumber, "<") {
		parts := strings.Split(callerNumber, "<")
		if len(parts) > 1 {
			uriPart := strings.Split(parts[1], "@")[0]
			uriPart = strings.TrimPrefix(uriPart, "sip:")
			callerNumber = uriPart
		}
	}

	findCtx, findCancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", event.TraceID), 10*time.Second)
	defer findCancel()

	findUserReq := &userv1.FindUserByContactRequest{
		ContactType:  "phone",
		ContactValue: callerNumber,
	}

	foundUserRes, err := um.userClient.FindUserByContact(findCtx, findUserReq)
	if err == nil && foundUserRes.User != nil {
		l.Info().Str("user_id", foundUserRes.User.Id).Msg("Mevcut kullanıcı bulundu.")
		var matchedContact *userv1.Contact
		for _, contact := range foundUserRes.User.Contacts {
			if contact.ContactValue == callerNumber {
				matchedContact = contact
				break
			}
		}
		return foundUserRes.User, matchedContact, nil
	}

	st, _ := status.FromError(err)
	if st.Code() == codes.NotFound {
		l.Info().Msg("Kullanıcı bulunamadı, yeni bir misafir kullanıcı oluşturulacak.")
		return um.createGuest(ctx, event, callerNumber)
	}

	l.Error().Err(err).Msg("Kullanıcı aranırken beklenmedik bir hata oluştu.")
	return nil, nil, err
}

func (um *UserManager) createGuest(ctx context.Context, event *state.CallEvent, callerNumber string) (*userv1.User, *userv1.Contact, error) {
	l := ctxlogger.FromContext(ctx)
	tenantID := "sentiric_demo"
	if event.Dialplan.GetInboundRoute() != nil && event.Dialplan.GetInboundRoute().TenantId != "" {
		tenantID = event.Dialplan.GetInboundRoute().TenantId
	} else {
		l.Warn().Msg("InboundRoute veya TenantId bulunamadı, fallback 'sentiric_demo' tenant'ı kullanılıyor.")
	}

	createCtx, createCancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", event.TraceID), 10*time.Second)
	defer createCancel()

	createUserReq := &userv1.CreateUserRequest{
		TenantId: tenantID,
		UserType: "caller",
		InitialContact: &userv1.CreateUserRequest_InitialContact{
			ContactType:  "phone",
			ContactValue: callerNumber,
		},
	}

	createdUserRes, createErr := um.userClient.CreateUser(createCtx, createUserReq)
	if createErr != nil {
		l.Error().Err(createErr).Msg("User-service'de misafir kullanıcı oluşturulamadı.")
		return nil, nil, createErr
	}

	l.Info().Str("user_id", createdUserRes.User.Id).Msg("Misafir kullanıcı başarıyla oluşturuldu.")
	var newContact *userv1.Contact
	if len(createdUserRes.User.Contacts) > 0 {
		newContact = createdUserRes.User.Contacts[0]
	}
	return createdUserRes.User, newContact, nil
}
