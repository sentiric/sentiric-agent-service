package constants

// DialogState, diyalog durumlarını tanımlar.
type DialogState string

const (
	StateWelcoming  DialogState = "WELCOMING"
	StateListening  DialogState = "LISTENING"
	StateThinking   DialogState = "THINKING"
	StateSpeaking   DialogState = "SPEAKING"
	StateEnded      DialogState = "ENDED"
	StateTerminated DialogState = "TERMINATED"
)

// EventType, RabbitMQ olay türlerini tanımlar.
type EventType string

const (
	EventTypeCallStarted           EventType = "call.started"
	EventTypeCallEnded             EventType = "call.ended"
	EventTypeUserIdentifiedForCall EventType = "user.identified.for_call"
	EventTypeCallTerminateRequest  EventType = "call.terminate.request"
)

// AnnouncementID, sistem anonslarını tanımlar.
type AnnouncementID string

const (
	AnnounceGuestWelcome         AnnouncementID = "ANNOUNCE_GUEST_WELCOME"
	AnnounceSystemConnecting     AnnouncementID = "ANNOUNCE_SYSTEM_CONNECTING"
	AnnounceSystemError          AnnouncementID = "ANNOUNCE_SYSTEM_ERROR"
	AnnounceSystemMaxFailures    AnnouncementID = "ANNOUNCE_SYSTEM_MAX_FAILURES"
	AnnounceSystemCantHearYou    AnnouncementID = "ANNOUNCE_SYSTEM_CANT_HEAR_YOU"
	AnnounceSystemCantUnderstand AnnouncementID = "ANNOUNCE_SYSTEM_CANT_UNDERSTAND"
	AnnounceSystemGoodbye        AnnouncementID = "ANNOUNCE_SYSTEM_GOODBYE"
)

// TemplateID, veritabanındaki prompt şablonlarını tanımlar.
type TemplateID string

const (
	PromptWelcomeKnownUser TemplateID = "PROMPT_WELCOME_KNOWN_USER"
	PromptWelcomeGuest     TemplateID = "PROMPT_WELCOME_GUEST"
	PromptSystemRAG        TemplateID = "PROMPT_SYSTEM_RAG"
	PromptSystemDefault    TemplateID = "PROMPT_SYSTEM_DEFAULT"
)
