// File: sentiric-agent-service/internal/database/postgres.go

package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"
)

// Connect, standartlaştırılmış, connection pooler'a dayanıklı bir veritabanı bağlantısı kurar.
func Connect(url string, log zerolog.Logger) (*sql.DB, error) {
	var db *sql.DB
	var err error

	// 1. URL'yi parse et
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		log.Fatal().Err(err).Msg("PostgreSQL URL parse edilemedi")
		return nil, err // Fatal zaten çıkar ama yine de ekleyelim
	}

	// 2. Connection Pooler ile uyumluluk için prepared statement'ları devre dışı bırak
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// 3. Yeni, yapılandırılmış URL ile bağlantıyı yeniden dene
	finalURL := stdlib.RegisterConnConfig(config.ConnConfig)

	for i := 0; i < 10; i++ {
		db, err = sql.Open("pgx", finalURL)
		if err == nil {
			// Bağlantı havuzu ayarları
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
		time.Sleep(5 * time.Second)
	}
	// 'Fatal' yerine hata döndürmek, çağıran fonksiyonun karar vermesine olanak tanır.
	// Ama main.go'da bu zaten fatal ile sonuçlanıyor, bu yüzden tutarlı.
	return nil, fmt.Errorf("maksimum deneme (%d) sonrası veritabanına bağlanılamadı: %w", 10, err)
}

// Bu servise özgü veritabanı fonksiyonları burada kalmaya devam eder.
func GetAnnouncementPathFromDB(db *sql.DB, announcementID, tenantID string) (string, error) {
	var audioPath string
	// SORGUNU GÜNCELLE: tenant_id'yi de içerecek şekilde
	query := `SELECT audio_path FROM announcements WHERE id = $1 AND (tenant_id = $2 OR tenant_id = 'system') ORDER BY tenant_id DESC LIMIT 1`
	err := db.QueryRow(query, announcementID, tenantID).Scan(&audioPath)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("anons bulunamadı: id=%s, tenant=%s", announcementID, tenantID)
		}
		return "", fmt.Errorf("anons sorgusu başarısız: %w", err)
	}
	return audioPath, nil
}

func GetTemplateFromDB(db *sql.DB, templateID, languageCode, tenantID string) (string, error) {
	var content string
	// SORGUNU GÜNCELLE: tenant_id'yi de içerecek şekilde
	query := "SELECT content FROM templates WHERE id = $1 AND language_code = $2 AND (tenant_id = $3 OR tenant_id = 'default') ORDER BY tenant_id DESC LIMIT 1"
	err := db.QueryRow(query, templateID, languageCode, tenantID).Scan(&content)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("şablon bulunamadı: id=%s, lang=%s, tenant=%s", templateID, languageCode, tenantID)
		}
		return "", fmt.Errorf("şablon sorgusu başarısız: %w", err)
	}
	return content, nil
}
