// sentiric-agent-service/internal/handler/call_handler.go

package handler

import (
	"context"
	"database/sql"
	"fmt"
	"time" // EKLENDİ

	"github.com/rs/zerolog"

	// Contracts v1.13.6
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"

	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type CallHandler struct {
	clients      *client.Clients
	stateManager *state.Manager
	db           *sql.DB
	log          zerolog.Logger
}

func NewCallHandler(clients *client.Clients, sm *state.Manager, db *sql.DB, log zerolog.Logger) *CallHandler {
	return &CallHandler{
		clients:      clients,
		stateManager: sm,
		db:           db,
		log:          log,
	}
}

func (h *CallHandler) HandleCallStarted(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	
	// [FIX] Idempotency Check: Aynı çağrı için işlem yapılıyor mu?
	// Redis'te basit bir kilit kontrolü yapıyoruz.
	lockKey := fmt.Sprintf("lock:call_started:%s", event.CallID)
	// RedisClient() metodunu Manager'a eklemiştik (state/manager.go)
	isNew, err := h.stateManager.RedisClient().SetNX(ctx, lockKey, "1", 10*time.Second).Result()
	
	if err != nil {
		l.Error().Err(err).Msg("Redis kilit hatası")
		return
	}
	
	if !isNew {
		l.Warn().Msg("⚠️ Çift 'call.started' olayı algılandı ve yoksayıldı.")
		return
	}

	l.Info().Msg("📞 Yeni çağrı yakalandı. Orkestrasyon başlıyor.")

	if event.Media == nil {
		l.Error().Msg("🚨 KRİTİK: Media bilgisi eksik, çağrı yönetilemez.")
		return
	}

	err = h.stateManager.Set(ctx, &state.CallState{
		CallID:       event.CallID,
		TraceID:      event.TraceID,
		Event:        event,
		CurrentState: constants.StateWelcoming,
	})
	if err != nil {
		l.Error().Err(err).Msg("Redis durum kaydı başarısız.")
	}

	if event.Dialplan == nil || event.Dialplan.Action == nil {
		l.Warn().Msg("⚠️ Dialplan çözülemedi veya aksiyon yok. Varsayılan (Misafir) akışı başlatılıyor.")
		h.startAIConversation(ctx, event, true) 
		return
	}

	action := event.Dialplan.Action.Action
	l.Info().Str("action", action).Msg("🎯 Dialplan kararı uygulanıyor.")

	switch action {
	case "START_AI_CONVERSATION":
		h.startAIConversation(ctx, event, false)
	case "PROCESS_GUEST_CALL":
		h.startAIConversation(ctx, event, true)
	case "PLAY_ANNOUNCEMENT":
		h.handlePlayAnnouncement(ctx, event)
	default:
		l.Warn().Str("unknown_action", action).Msg("❓ Bilinmeyen aksiyon. Varsayılan akışa dönülüyor.")
		h.startAIConversation(ctx, event, true)
	}
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	h.log.Info().Str("call_id", event.CallID).Msg("📴 Çağrı sonlandı.")
	// Kilidi temizle (opsiyonel, zaten TTL var)
}

// --- ALT MANTIKLAR (SUB-LOGIC) ---
// bu görişme mantıkları saga ile mi işleyecek agent ui sinden mi yönetilecek bu tarz işlemler 
// yoksa dial plan kurallarına göre mi dial plan lar iin ayrı bir ui yapacakmıyız
// aslında her serrvisi bağımsız ve yönetilebilir ve düzenli olşuturmaya çalışıoruz.

func (h *CallHandler) startAIConversation(ctx context.Context, event *state.CallEvent, isGuest bool) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	// bu kısmı neden hardcode yazmışız? 
	// anouncemınlarda default seslerimiz var mesala oaraya hem bu metinler girilir hemde wav dosyaları ile uyumlu olur
	// böylece hem gerektiğinde wav dosyasını kullanırız gerektiğinde tts tarafından okunmasını sağlayabiliriz?
	// bu en basit yaklaşım. Üzerinde daha da değerlendirme yapalım.
	// ayrıca sentirik türkçe okunuşlarında doğru telefuz için k ile yazılmalı
	welcomeText := "Merhaba, Sentirik iletişim sistemine hoş geldiniz."
	// genelde burada hep deault olarak tanımlıyoruz ancak default olan ses hangisi belli mi tts de.
	// voiceID := "coqui:default"
	// parlatk zeynep i kullandık.
	// bu default ses olabilir hem türkçe hem ingilizce için?
	// stream gatewayde tts default voice diye bir tanım ile bunu kullanıoruz
	// tts den belkide tüm sesleri alabiliriz?
	// aslında bize bir mini ui lazım agent da bu tarz değişiklikleri yapabilmek için 
	// tabiki compose env lerinde de tanımlanabilir.
	// ama hardcode olmaması lazım 
	// db den çekmekte mantıklı eğer ui kullanacakisek 
	voiceID := "coqui:F_TR_Parlak_Zeynep/neutral"
	
	if !isGuest && event.Dialplan != nil && event.Dialplan.MatchedUser != nil {
		// bu sabit değeri nereden bulduk
		// databaseden mi çekiyoruz?
		// eğer otomaitk kaydediyor isek role kısmına göre yapmak daha mantıklı 
		// örneğin agent admin gibi roller belirlemiş zaten buraası için guest olabilir
		// ama hardcode olmaması lazım
		userName := "Misafir"
		if event.Dialplan.MatchedUser.Name != nil {
			userName = *event.Dialplan.MatchedUser.Name
		}
		// tekrar hoşgeldiniz bu kayıt lı olmayan kullanıcı için mi diyoruz. daha once konustuk ve türkçe konuşan bir kullanıcımı ?
		// bu dil konusunuda çözümlenmesi grek. neye göre kullanıcı ile iletişime başlayacağız
		// evet varsayılan başlangıcımız her zaman türkçe olacak ama kullanıcı dil tercihi var ise ona göre de başlatabiliriz
		welcomeText = fmt.Sprintf("Merhaba %s, tekrar hoş geldiniz. Size nasıl yardımcı olabilirim?", userName)
	}

	l.Info().Msg("🗣️  AI Karşılama başlatılıyor...")

	mediaInfoProto := &eventv1.MediaInfo{
		CallerRtpAddr: event.Media.CallerRtpAddr,
		ServerRtpPort: uint32(event.Media.ServerRtpPort),
	}

	req := &telephonyv1.SpeakTextRequest{
		CallId:    event.CallID,
		Text:      welcomeText,
		VoiceId:   voiceID,
		MediaInfo: mediaInfoProto,
	}

	_, err := h.clients.TelephonyAction.SpeakText(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ SpeakText başarısız oldu. Fallback anons çalınıyor.")
		h.playAnnouncementAndHangup(ctx, event.CallID, "ANNOUNCE_SYSTEM_ERROR", "system", "tr", event.Media)
		return
	}
	l.Info().Msg("✅ SpeakText iletildi. (Not: STT tetiklemesi TelephonyAction tarafından yönetilecek)")
}

func (h *CallHandler) handlePlayAnnouncement(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	
	announceID := "ANNOUNCE_GENERIC"
	lang := "tr"
	tenantID := "system"

	if event.Dialplan != nil {
		tenantID = event.Dialplan.TenantID
		if event.Dialplan.Action != nil && event.Dialplan.Action.ActionData != nil {
			if val, ok := event.Dialplan.Action.ActionData.Data["announcementId"]; ok {
				announceID = val
			}
		}
	}

	l.Info().Str("announce_id", announceID).Msg("📢 Anons çalma isteği.")
	h.playAnnouncementAndHangup(ctx, event.CallID, announceID, tenantID, lang, event.Media)
}

func (h *CallHandler) playAnnouncementAndHangup(ctx context.Context, callID, announceID, tenantID, lang string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Str("announce_id", announceID).Logger()

	if h.db == nil {
		l.Error().Msg("DB bağlantısı yok, anons çalınamıyor.")
		return
	}

	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announceID, tenantID, lang)
	if err != nil {
		l.Error().Err(err).Msg("Anons dosyası bulunamadı, varsayılan hata sesi çalınıyor.")
		audioPath = "audio/tr/system/technical_difficulty.wav" 
	}

	fullURI := fmt.Sprintf("file://%s", audioPath)
	
	req := &telephonyv1.PlayAudioRequest{
		CallId:   callID,
		AudioUri: fullURI,
	}

	_, err = h.clients.TelephonyAction.PlayAudio(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ Anons komutu iletilemedi.")
	} else {
		l.Info().Str("uri", fullURI).Msg("✅ PlayAudio komutu gönderildi.")
	}
}