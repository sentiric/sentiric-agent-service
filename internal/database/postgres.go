package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"
)

// Connect, anında döner ve arka planda ping atarak bağlantıyı bekler
func Connect(ctx context.Context, url string, log zerolog.Logger) *sql.DB {
	config, _ := pgxpool.ParseConfig(url)
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	finalURL := stdlib.RegisterConnConfig(config.ConnConfig)

	db, _ := sql.Open("pgx", finalURL)
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(10)

	go func() {
		for {
			pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := db.PingContext(pingCtx)
			cancel()
			if err == nil {
				log.Info().Str("event", "POSTGRES_CONNECTED").Msg("✅ Veritabanına bağlantı başarılı (Simple Protocol Mode).")
				return
			}
			log.Warn().Str("event", "POSTGRES_RETRY").Err(err).Msg("PostgreSQL ulaşılamıyor, 5 saniye sonra tekrar denenecek...")
			time.Sleep(5 * time.Second)
		}
	}()

	return db
}

func ConnectRedis(ctx context.Context, url string, log zerolog.Logger) *redis.Client {
	opt, _ := redis.ParseURL(url)
	rdb := redis.NewClient(opt)

	go func() {
		for {
			pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := rdb.Ping(pingCtx).Err()
			cancel()
			if err == nil {
				log.Info().Str("event", "REDIS_CONNECTED").Msg("✅ Redis bağlantısı başarılı.")
				return
			}
			log.Warn().Str("event", "REDIS_RETRY").Err(err).Msg("Redis ulaşılamıyor, 5 saniye sonra tekrar denenecek...")
			time.Sleep(5 * time.Second)
		}
	}()

	return rdb
}

func GetAnnouncementPathFromDB(db *sql.DB, announcementID, tenantID, languageCode string) (string, error) {
	var audioPath string
	query := `SELECT audio_path FROM announcements WHERE id = $1 AND language_code = $2 AND (tenant_id = $3 OR tenant_id = 'system') ORDER BY tenant_id DESC LIMIT 1`
	err := db.QueryRow(query, announcementID, languageCode, tenantID).Scan(&audioPath)
	return audioPath, err
}

func GetTemplateFromDB(db *sql.DB, templateID, languageCode, tenantID string) (string, error) {
	var content string
	query := `SELECT content FROM templates WHERE id = $1 AND language_code = $2 AND (tenant_id = $3 OR tenant_id = 'system') ORDER BY tenant_id DESC LIMIT 1`
	err := db.QueryRow(query, templateID, languageCode, tenantID).Scan(&content)
	return content, err
}

func CreateConversation(db *sql.DB, callID, tenantID string, channel string) error {
	query := `INSERT INTO conversations (call_id, tenant_id, channel, status, created_at) VALUES ($1, $2, $3, 'ACTIVE', NOW()) ON CONFLICT (id) DO NOTHING`
	_, err := db.Exec(query, callID, tenantID, channel)
	return err
}

func UpdateConversationStatus(db *sql.DB, callID, status string) error {
	query := `UPDATE conversations SET status = $1, updated_at = NOW() WHERE call_id = $2`
	_, err := db.Exec(query, status, callID)
	return err
}

func AddTranscript(db *sql.DB, callID, senderType, message string) error {
	var convID string
	err := db.QueryRow("SELECT id FROM conversations WHERE call_id = $1 ORDER BY created_at DESC LIMIT 1", callID).Scan(&convID)
	if err != nil {
		return err
	}
	query := `INSERT INTO transcripts (conversation_id, sender_type, message_text, created_at) VALUES ($1, $2, $3, NOW())`
	_, err = db.Exec(query, convID, senderType, message)
	return err
}
