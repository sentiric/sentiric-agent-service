// sentiric-agent-service/internal/database/postgres.go

package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"
)

const (
	maxRetries  = 10
	retryDelay  = 5 * time.Second
	pingTimeout = 5 * time.Second
)

// Connect, yeniden deneme mekanizması ile PostgreSQL'e bağlanır.
func Connect(ctx context.Context, url string, log zerolog.Logger) (*sql.DB, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		// DÜZELTME (ST1005): Hata mesajı küçük harfle başlar.
		return nil, fmt.Errorf("postgresql URL parse edilemedi: %w", err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	finalURL := stdlib.RegisterConnConfig(config.ConnConfig)

	var db *sql.DB
	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		db, err = sql.Open("pgx", finalURL)
		if err == nil {
			db.SetConnMaxLifetime(time.Minute * 3)
			db.SetMaxIdleConns(5)
			db.SetMaxOpenConns(10)

			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			pingErr := db.PingContext(pingCtx)
			cancel()

			if pingErr == nil {
				log.Info().Msg("Veritabanına bağlantı başarılı (Simple Protocol Mode).")
				return db, nil
			}
			err = pingErr
			db.Close()
		}

		if ctx.Err() == nil {
			log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", maxRetries).Msg("Veritabanına bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		}

		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("maksimum deneme (%d) sonrası veritabanına bağlanılamadı: %w", maxRetries, err)
}

// ConnectRedis, yeniden deneme mekanizması ile Redis'e bağlanır.
func ConnectRedis(ctx context.Context, url string, log zerolog.Logger) (*redis.Client, error) {
	var rdb *redis.Client
	var err error

	opt, parseErr := redis.ParseURL(url)
	if parseErr != nil {
		// DÜZELTME (ST1005): Hata mesajı küçük harfle başlar.
		return nil, fmt.Errorf("redis URL parse edilemedi: %w", parseErr)
	}

	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		rdb = redis.NewClient(opt)
		pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
		pingErr := rdb.Ping(pingCtx).Err()
		cancel()

		if pingErr == nil {
			log.Info().Msg("Redis bağlantısı başarılı.")
			return rdb, nil
		}
		err = pingErr
		rdb.Close()

		if ctx.Err() == nil {
			log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", maxRetries).Msg("Redis'e bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		}

		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("maksimum deneme (%d) sonrası Redis'e bağlanılamadı: %w", maxRetries, err)
}

func GetAnnouncementPathFromDB(db *sql.DB, announcementID, tenantID, languageCode string) (string, error) {
	var audioPath string
	query := `
        SELECT audio_path FROM announcements
        WHERE id = $1 AND language_code = $2 AND (tenant_id = $3 OR tenant_id = 'system')
        ORDER BY tenant_id DESC LIMIT 1`
	err := db.QueryRow(query, announcementID, languageCode, tenantID).Scan(&audioPath)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("anons bulunamadı: id=%s, tenant=%s, lang=%s", announcementID, tenantID, languageCode)
		}
		return "", fmt.Errorf("anons sorgusu başarısız: %w", err)
	}
	return audioPath, nil
}

func GetTemplateFromDB(db *sql.DB, templateID, languageCode, tenantID string) (string, error) {
	var content string
	query := `
		SELECT content FROM templates
		WHERE id = $1 AND language_code = $2 AND (tenant_id = $3 OR tenant_id = 'system')
		ORDER BY tenant_id DESC LIMIT 1`
	err := db.QueryRow(query, templateID, languageCode, tenantID).Scan(&content)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("şablon bulunamadı: id=%s, lang=%s, tenant=%s", templateID, languageCode, tenantID)
		}
		return "", fmt.Errorf("şablon sorgusu başarısız: %w", err)
	}
	return content, nil
}
