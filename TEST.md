Şimdi sırasıyla aşağıdaki komutları çalıştırın. Hata almamalısınız.

1.  **Derleme:**
    ```bash
    go mod tidy
    go build -o bin/agent-service ./cmd/agent-service
    ```

2.  **Ortamı Başlatma (Full Stack):**
    ```bash
    docker compose up --build -d
    ```

3.  **Log Takibi:**
    ```bash
    docker compose logs -f agent-service
    ```

4.  **Test Olayı Gönderme (Ayrı terminalde):**
    ```bash
    go run examples/publish_event.go
    ```
    *(Eğer localhost RabbitMQ'ya erişemezse, RabbitMQ portunun (5672) dışarı açık olduğundan emin olun veya docker exec ile container içinden çalıştırın)*

"Omniscient Architect Modu: Agent Service kodları düzeltildi. Test edip onayladığınızda, sistemin tüm parçalarını birleştiren **Ana Orkestrasyon** aşamasına hazırız."