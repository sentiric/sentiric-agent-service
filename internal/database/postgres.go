// AÇIKLAMA: Bu paket, veritabanı bağlantısı ve sorgulama işlemlerinden sorumludur.
package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver
	"github.com/rs/zerolog"
)

// Connect, veritabanına yeniden deneme mekanizması ile bağlanır.
func Connect(url string, log zerolog.Logger) (*sql.DB, error) {
	var db *sql.DB
	var err error
	for i := 0; i < 10; i++ {
		db, err = sql.Open("pgx", url)
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

// GetAnnouncementPathFromDB, veritabanından anonsun ses dosya yolunu alır.
func GetAnnouncementPathFromDB(db *sql.DB, announcementID string) (string, error) {
	var audioPath string
	query := "SELECT audio_path FROM announcements WHERE id = $1"
	err := db.QueryRow(query, announcementID).Scan(&audioPath)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("anons bulunamadı: %s", announcementID)
		}
		return "", fmt.Errorf("anons sorgusu başarısız: %w", err)
	}
	return audioPath, nil
}
