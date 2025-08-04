// DOSYA: sentiric-agent-service/cmd/testclient/main.go

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	log.Println("--- Go mTLS Test İstemcisi (agent-service -> user-service) ---")

	// .env dosyasını projenin kök dizininden yükle
	if err := godotenv.Load("./.env"); err != nil {
		log.Printf("Uyarı: .env dosyası bulunamadı, ortam değişkenlerine güveniliyor. Hata: %v", err)
	}

	// Ortam değişkenlerini al
	userServiceURL := os.Getenv("USER_SERVICE_GRPC_URL")
	agentCertPath := os.Getenv("AGENT_SERVICE_CERT_PATH")
	agentKeyPath := os.Getenv("AGENT_SERVICE_KEY_PATH")
	caPath := os.Getenv("GRPC_TLS_CA_PATH")

	if userServiceURL == "" || agentCertPath == "" || agentKeyPath == "" || caPath == "" {
		log.Fatal("Gerekli ortam değişkenlerinden biri veya birkaçı eksik!")
	}

	log.Printf("Bağlanılacak Adres: %s", userServiceURL)
	log.Printf("Kullanılacak İstemci Sertifikası: %s", agentCertPath)

	// İstemci için mTLS kimlik bilgilerini yükle
	creds, err := loadClientTLS(agentCertPath, agentKeyPath, caPath)
	if err != nil {
		log.Fatalf("İstemci TLS kimlik bilgileri yüklenemedi: %v", err)
	}

	// Güvenli gRPC bağlantısı kur
	log.Println("user-service'e bağlanılıyor...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, userServiceURL, grpc.WithTransportCredentials(creds), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Bağlantı başarısız: %v", err)
	}
	defer conn.Close()

	log.Println("✅ Bağlantı başarılı! mTLS handshake tamamlandı.")

	// Test için bir RPC çağrısı yap
	client := userv1.NewUserServiceClient(conn)
	testUserID := "905548777858" // veritabanında olan bir kullanıcı

	log.Printf("'%s' ID'li kullanıcı için GetUser RPC çağrısı yapılıyor...", testUserID)

	res, err := client.GetUser(ctx, &userv1.GetUserRequest{Id: testUserID})
	if err != nil {
		log.Fatalf("GetUser RPC çağrısı başarısız: %v", err)
	}

	log.Printf("✅ RPC çağrısı başarılı! Dönen kullanıcı adı: %s", res.GetUser().GetName())
	log.Println("--- Simülasyon Tamamlandı ---")
}

// Bu fonksiyon, bir gRPC İSTEMCİSİ için TLS yapılandırması oluşturur.
func loadClientTLS(certPath, keyPath, caPath string) (credentials.TransportCredentials, error) {
	// İstemcinin kendi sertifikasını ve anahtarını yükle
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("istemci sertifikası yüklenemedi: %w", err)
	}

	// Sunucuyu doğrulamak için kullanacağımız Kök CA sertifikasını oku
	caCert, err := ioutil.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("CA sertifikası okunamadı: %w", err)
	}

	// Güvenilir sunucu CA'larını içeren bir havuz oluştur
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("CA sertifikası havuza eklenemedi")
	}

	creds := credentials.NewTLS(&tls.Config{
		// Sunucuya kendimizi tanıtmak için kullanacağımız sertifika
		Certificates: []tls.Certificate{certificate},
		// Sunucunun sertifikasını doğrulamak için kullanacağımız CA havuzu
		RootCAs: caPool,
		// Sunucu sertifikasındaki CN/SAN alanının bu değerle eşleşmesi gerekir.
		// Docker network'ünde servis adı kullanılır.
		ServerName: "user-service",
	})

	return creds, nil
}
