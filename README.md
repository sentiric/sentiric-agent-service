# 🧠 Sentiric Agent Service

**Description:** This service is the **central brain** of the Sentiric platform. Written in **Python**, it is responsible for managing the entire asynchronous dialogue flow of a call, orchestrating various AI services, and executing business logic.

**Core Responsibilities:**
*   **Event Consumption:** Listens to the `call.events` queue on **RabbitMQ** for new events, such as `call.started`.
*   **Dialogue Management:** Manages the state of each conversation using the `CallContext` model (SMCP).
*   **AI Orchestration:** Acts as a client to other specialized services to perform tasks:
    *   Calls `sentiric-stt-service` to transcribe user speech.
    *   Calls an LLM (e.g., Gemini, GPT) to understand user intent and generate responses.
    *   Calls `sentiric-tts-service` to synthesize speech from text.
    *   Calls `sentiric-media-service` to play audio to the user.
*   **Business Logic Execution:** Triggers business workflows by calling services like `sentiric-connectors-service` (e.g., to book an appointment in a CRM).

**Technology Stack:**
*   **Language:** Python
*   **Framework (Future):** FastAPI (for potential internal API endpoints)
*   **Inter-Service Communication:**
    *   **AMQP (with Pika):** Consumes events from RabbitMQ.
    *   **REST/gRPC (Future):** Will act as a client to other AI and core services.
*   **Logging:** `structlog` for structured JSON logging.

**API Interactions:**
This service primarily acts as an **event consumer** and an **API client**. It is the main orchestrator that calls upon all other AI and business logic services.

## Getting Started

### Prerequisites
- Docker and Docker Compose
- Git
- All Sentiric repositories cloned into a single workspace directory.

### Local Development & Platform Setup
This service is not designed to run standalone. It is an integral part of the Sentiric platform and must be run via the central orchestrator in the `sentiric-infrastructure` repository.

1.  **Clone all repositories:**
    ```bash
    # In your workspace directory
    git clone https://github.com/sentiric/sentiric-infrastructure.git
    git clone https://github.com/sentiric/sentiric-agent-service.git
    # ... clone other required services
    ```

2.  **Configure Environment:**
    ```bash
    cd sentiric-infrastructure
    cp .env.local.example .env
    # Ensure RABBITMQ_URL is correctly set in the .env file
    ```

3.  **Run the entire platform:** The central Docker Compose file will automatically build and run this service.
    ```bash
    # From the sentiric-infrastructure directory
    docker compose up --build -d
    ```

4.  **View Logs:** To see the structured JSON logs from this service:
    ```bash
    docker compose logs -f agent-service
    ```

## Configuration

All configuration is managed via environment variables passed from the `sentiric-infrastructure` repository's `.env` file. The primary variable for this service is `RABBITMQ_URL`.

## Deployment

This service is designed for containerized deployment. The `Dockerfile` creates a minimal image based on `python-slim`. The CI/CD pipeline in `.github/workflows/docker-ci.yml` automatically builds and pushes the image to the GitHub Container Registry (`ghcr.io`).

## Contributing

We welcome contributions! Please refer to the [Sentiric Governance](https://github.com/sentiric/sentiric-governance) repository for detailed coding standards, contribution guidelines, and the overall project vision.

## License

This project is licensed under the [License](LICENSE).

