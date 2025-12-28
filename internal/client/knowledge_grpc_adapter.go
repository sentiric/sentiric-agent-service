// sentiric-agent-service/internal/client/knowledge_grpc_adapter.go
package client

import (
	"context"

	knowledgev1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/knowledge/v1"
)

// GrpcKnowledgeClientAdapter, otomatik üretilen gRPC istemcisini
// servis katmanında tanımlanan temiz arayüze (service.KnowledgeClientInterface) uyumlu hale getirir.
type GrpcKnowledgeClientAdapter struct {
	// DÜZELTME: Tip KnowledgeQueryServiceClient olarak güncellendi.
	client knowledgev1.KnowledgeQueryServiceClient
}

// NewGrpcKnowledgeClientAdapter, yeni bir adaptör örneği oluşturur.
// Parametre olarak asıl gRPC istemcisini alır.
// DÜZELTME: Parametre tipi KnowledgeQueryServiceClient olarak güncellendi.
func NewGrpcKnowledgeClientAdapter(client knowledgev1.KnowledgeQueryServiceClient) *GrpcKnowledgeClientAdapter {
	return &GrpcKnowledgeClientAdapter{client: client}
}

// Query, service.KnowledgeClientInterface arayüzünü uygular.
func (a *GrpcKnowledgeClientAdapter) Query(ctx context.Context, req *knowledgev1.QueryRequest) (*knowledgev1.QueryResponse, error) {
	// Adaptasyon burada gerçekleşiyor: Temiz arayüz metodundan, gRPC istemci metoduna çağrı yapılıyor.
	return a.client.Query(ctx, req)
}