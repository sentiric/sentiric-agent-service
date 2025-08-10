// AÇIKLAMA: Bu paket, Prometheus metriklerini tanımlar ve /metrics
// endpoint'ini sunan bir HTTP sunucusu başlatır.
package metrics

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

var (
	// EventsProcessed, işlenen olayların sayısını tutar.
	EventsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sentiric_agent_events_processed_total",
			Help: "İşlenen toplam olay sayısı.",
		},
		[]string{"event_type"},
	)
	// EventsFailed, işlenirken hata alınan olayların sayısını tutar.
	EventsFailed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sentiric_agent_events_failed_total",
			Help: "İşlenirken hata alınan toplam olay sayısı.",
		},
		[]string{"event_type", "reason"},
	)
)

// StartServer, metrikleri sunmak için bir HTTP sunucusu başlatır.
func StartServer(port string, log zerolog.Logger) {
	addr := fmt.Sprintf(":%s", port)
	log.Info().Str("address", addr).Msg("Metrik sunucusu başlatılıyor...")

	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("Metrik sunucusu başlatılamadı")
	}
}
