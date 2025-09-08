package database

import (
	"context" // YENİ İMPORT
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"
)

// DEĞİŞİKLİK: Fonksiyon artık context alıyor.
func Connect(ctx context.Context, url string, log zerolog.Logger) (*sql.DB, error) {
	var db *sql.DB
	var err error

	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("PostgreSQL URL parse edilemedi: %w", err)
	}

	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	finalURL := stdlib.RegisterConnConfig(config.ConnConfig)

	for i := 0; i < 10; i++ {
		// DEĞİŞİKLİK: Döngünün başında context'in iptal edilip edilmediğini kontrol et.
		select {
		case <-ctx.Done():
			return nil, ctx.Err() // Context iptal edildiyse hatayla çık.
		default:
			// Context iptal edilmediyse devam et.
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
		log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", 10).Msg("Veritabanına bağlanılamadı, 5 saniye sonra tekrar denenecek...")

		// DEĞİŞİKLİK: time.Sleep yerine context-aware bekleme yapılıyor.
		select {
		case <-time.After(5 * time.Second):
			// 5 saniye doldu, döngüye devam et.
		case <-ctx.Done():
			return nil, ctx.Err() // Bekleme sırasında context iptal edilirse hatayla çık.
		}
	}
	return nil, fmt.Errorf("maksimum deneme (%d) sonrası veritabanına bağlanılamadı: %w", 10, err)
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
