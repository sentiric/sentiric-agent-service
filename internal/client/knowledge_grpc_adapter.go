package client

import (
	"context"

	knowledgev1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/knowledge/v1"
)

// GrpcKnowledgeClientAdapter, otomatik üretilen gRPC istemcisini (knowledgev1.KnowledgeServiceClient)
// servis katmanında tanımlanan temiz arayüze (service.KnowledgeClientInterface) uyumlu hale getirir.
type GrpcKnowledgeClientAdapter struct {
	client knowledgev1.KnowledgeServiceClient
}

// NewGrpcKnowledgeClientAdapter, yeni bir adaptör örneği oluşturur.
// Parametre olarak asıl gRPC istemcisini alır.
func NewGrpcKnowledgeClientAdapter(client knowledgev1.KnowledgeServiceClient) *GrpcKnowledgeClientAdapter {
	return &GrpcKnowledgeClientAdapter{client: client}
}

// Query, service.KnowledgeClientInterface arayüzünü uygular.
// İçeride, asıl gRPC istemcisinin Query metodunu çağırır.
// gRPC istemcisinin beklediği ...grpc.CallOption parametresi variadic olduğu için
// buraya herhangi bir opsiyon geçmeden çağrı yapabiliriz.
func (a *GrpcKnowledgeClientAdapter) Query(ctx context.Context, req *knowledgev1.QueryRequest) (*knowledgev1.QueryResponse, error) {
	// Adaptasyon burada gerçekleşiyor: Temiz arayüz metodundan, gRPC istemci metoduna çağrı yapılıyor.
	return a.client.Query(ctx, req)
}
