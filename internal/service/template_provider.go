package service

import (
	"context"
	"database/sql"
	"strings"

	"github.com/sentiric/sentiric-agent-service/internal/ctxlogger"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

// TemplateProvider, veritabanından prompt şablonlarını almakla sorumludur.
type TemplateProvider struct {
	db *sql.DB
}

// NewTemplateProvider, yeni bir TemplateProvider örneği oluşturur.
func NewTemplateProvider(db *sql.DB) *TemplateProvider {
	return &TemplateProvider{db: db}
}

// GetWelcomePrompt, kullanıcı durumuna göre uygun karşılama prompt'unu döndürür.
func (tp *TemplateProvider) GetWelcomePrompt(ctx context.Context, callState *state.CallState) (string, error) {
	l := ctxlogger.FromContext(ctx)
	languageCode := getLanguageCode(callState.Event)
	var promptID string
	if callState.Event.Dialplan.MatchedUser != nil && callState.Event.Dialplan.MatchedUser.Name != nil {
		promptID = "PROMPT_WELCOME_KNOWN_USER"
	} else {
		promptID = "PROMPT_WELCOME_GUEST"
	}
	promptTemplate, err := database.GetTemplateFromDB(tp.db, promptID, languageCode, callState.TenantID)
	if err != nil {
		l.Error().Err(err).Msg("Prompt şablonu veritabanından alınamadı.")
		return "Merhaba, hoş geldiniz.", err
	}
	prompt := promptTemplate
	if callState.Event.Dialplan.MatchedUser != nil && callState.Event.Dialplan.MatchedUser.Name != nil {
		prompt = strings.Replace(prompt, "{user_name}", *callState.Event.Dialplan.MatchedUser.Name, -1)
	}
	return prompt, nil
}

// BuildLlmPrompt, konuşma geçmişini kullanarak LLM için tam bir prompt oluşturur.
func (tp *TemplateProvider) BuildLlmPrompt(ctx context.Context, callState *state.CallState) (string, error) {
	l := ctxlogger.FromContext(ctx)
	languageCode := getLanguageCode(callState.Event)
	systemPrompt, err := database.GetTemplateFromDB(tp.db, "PROMPT_SYSTEM_DEFAULT", languageCode, callState.TenantID)
	if err != nil {
		l.Error().Err(err).Msg("Sistem prompt'u alınamadı, fallback kullanılıyor.")
		systemPrompt = "Aşağıdaki diyaloğa devam et. Cevapların kısa olsun."
	}
	var promptBuilder strings.Builder
	promptBuilder.WriteString(systemPrompt)
	promptBuilder.WriteString("\n\n--- KONUŞMA GEÇMİŞİ ---\n")
	for _, msg := range callState.Conversation {
		if text, ok := msg["user"]; ok {
			promptBuilder.WriteString("Kullanıcı: " + text + "\n")
		} else if text, ok := msg["ai"]; ok {
			promptBuilder.WriteString("Asistan: " + text + "\n")
		}
	}
	promptBuilder.WriteString("Asistan:")
	return promptBuilder.String(), nil
}
