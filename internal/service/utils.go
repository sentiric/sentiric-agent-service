package service

import (
	"github.com/sentiric/sentiric-agent-service/internal/state"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
)

// GetLanguageCode, bir çağrı olayı içinden en uygun dil kodunu çıkarır.
func GetLanguageCode(event *state.CallEvent) string {
	if event != nil && event.Dialplan != nil && event.Dialplan.MatchedUser != nil {
		if user := event.Dialplan.MatchedUser; user.PreferredLanguageCode != nil && *user.PreferredLanguageCode != "" {
			return *user.PreferredLanguageCode
		}
	}
	return "tr"
}

// ConvertUserToPayload, gRPC'den gelen User nesnesini state'te kullanılacak Payload nesnesine dönüştürür.
func ConvertUserToPayload(user *userv1.User) *state.MatchedUserPayload {
	if user == nil {
		return nil
	}
	var contacts []*state.MatchedContactPayload
	for _, c := range user.Contacts {
		contacts = append(contacts, ConvertContactToPayload(c))
	}
	return &state.MatchedUserPayload{
		ID:                    user.Id,
		Name:                  user.Name,
		TenantID:              user.TenantId,
		UserType:              user.UserType,
		Contacts:              contacts,
		PreferredLanguageCode: user.PreferredLanguageCode,
	}
}

// ConvertContactToPayload, gRPC'den gelen Contact nesnesini state'te kullanılacak Payload nesnesine dönüştürür.
func ConvertContactToPayload(contact *userv1.Contact) *state.MatchedContactPayload {
	if contact == nil {
		return nil
	}
	return &state.MatchedContactPayload{
		ID:           contact.Id,
		UserID:       contact.UserId,
		ContactType:  contact.ContactType,
		ContactValue: contact.ContactValue,
		IsPrimary:    contact.IsPrimary,
	}
}
