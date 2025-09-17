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
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"
)

func Connect(ctx context.Context, url string, log zerolog.Logger) (*sql.DB, error) {
	var db *sql.DB
	var err error
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("PostgreSQL URL parse edilemedi: %w", err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	finalURL := stdlib.RegisterConnConfig(config.ConnConfig)
	for {
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
			if pingErr := db.Ping(); pingErr == nil {
				log.Info().Msg("Veritabanına bağlantı başarılı (Simple Protocol Mode).")
				return db, nil
			} else {
				err = pingErr
			}
		}
		if ctx.Err() == nil {
			log.Warn().Err(err).Msg("Veritabanına bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		}
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func ConnectRedis(ctx context.Context, url string, log zerolog.Logger) (*redis.Client, error) {
	var rdb *redis.Client
	var err error
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		opt, parseErr := redis.ParseURL(url)
		if parseErr != nil {
			err = parseErr
		} else {
			rdb = redis.NewClient(opt)
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			pingErr := rdb.Ping(pingCtx).Err()
			cancel()
			if pingErr == nil {
				log.Info().Msg("Redis bağlantısı başarılı.")
				return rdb, nil
			}
			err = pingErr
		}
		if ctx.Err() == nil {
			log.Warn().Err(err).Msg("Redis'e bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		}
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
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
