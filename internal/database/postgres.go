package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"
)

func Connect(url string, log zerolog.Logger) (*sql.DB, error) {
	var db *sql.DB
	var err error

	finalURL := url
	if !strings.Contains(finalURL, "statement_cache_mode") {
		separator := "?"
		if strings.Contains(finalURL, "?") {
			separator = "&"
		}
		finalURL = fmt.Sprintf("%s%sstatement_cache_mode=disable", finalURL, separator)
	}

	for i := 0; i < 10; i++ {
		db, err = sql.Open("pgx", finalURL)
		if err == nil {
			if err = db.Ping(); err == nil {
				log.Info().Msg("Veritabanı bağlantısı başarılı.")
				return db, nil
			}
		}
		log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", 10).Msg("Veritabanına bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		time.Sleep(5 * time.Second)
	}
	return nil, fmt.Errorf("maksimum deneme (%d) sonrası veritabanına bağlanılamadı: %w", 10, err)
}

// ... (dosyanın geri kalan fonksiyonları aynı kalacak) ...
func GetAnnouncementPathFromDB(db *sql.DB, announcementID string) (string, error) {
	var audioPath string
	query := `SELECT audio_path FROM announcements WHERE id = $1 LIMIT 1`
	err := db.QueryRow(query, announcementID).Scan(&audioPath)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("anons bulunamadı: id=%s", announcementID)
		}
		return "", fmt.Errorf("anons sorgusu başarısız: %w", err)
	}
	return audioPath, nil
}

func GetTemplateFromDB(db *sql.DB, templateID, languageCode string) (string, error) {
	var content string
	query := "SELECT content FROM templates WHERE id = $1 AND language_code = $2 AND tenant_id = 'default'"
	err := db.QueryRow(query, templateID, languageCode).Scan(&content)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("şablon bulunamadı: id=%s, lang=%s", templateID, languageCode)
		}
		return "", fmt.Errorf("şablon sorgusu başarısız: %w", err)
	}
	return content, nil
}
