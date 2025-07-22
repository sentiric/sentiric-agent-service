# Sentiric Agent Service

**Description:** The core intelligence service of the Sentiric platform, responsible for managing dialogue flows, coordinating various AI services, and generating appropriate responses for human-machine interactions.

**Core Responsibilities:**
*   Processing transcribed text from `sentiric-stt-service`.
*   Executing dialogue management logic (understanding user intent, tracking dialogue state).
*   Querying `sentiric-knowledge-service` for relevant information.
*   Triggering `sentiric-connectors` to interact with external business systems.
*   Generating text responses (including Large Language Model - LLM integration).
*   Utilizing `sentiric-tts-service` for spoken responses or sending text responses to `sentiric-messaging-gateway`.

**Technologies:**
*   Python (or Node.js)
*   Flask/FastAPI (for REST API)
*   Libraries for NLP, LLM orchestration, dialogue management.
* we can use Rasa + DistilBERT (ONNX quantized)

**API Interactions (As an API Provider & Client):**
*   **As a Client:** Calls `sentiric-stt-service`, `sentiric-tts-service`, `sentiric-knowledge-service`, `sentiric-connectors`.
*   **As a Provider:** Exposes APIs for `sentiric-api-gateway-service` (for general access) and `sentiric-messaging-gateway` (for text responses).

**Local Development:**
1.  Clone this repository: `git clone https://github.com/sentiric/sentiric-agent-service.git`
2.  Navigate into the directory: `cd sentiric-agent-service`
3.  Create a virtual environment and install dependencies: `python -m venv venv && source venv/bin/activate && pip install -r requirements.txt`
4.  Create a `.env` file from `.env.example` to configure API URLs for dependent AI services.
5.  Start the service: `python app.py` (or equivalent).

**Configuration:**
Refer to `config/` directory and `.env.example` for service-specific configurations, including dialogue flows and AI model parameters.

**Deployment:**
Designed for containerized deployment (e.g., Docker, Kubernetes), potentially scaling based on dialogue complexity and volume. Refer to `sentiric-infrastructure`.

**Contributing:**
We welcome contributions! Please refer to the [Sentiric Governance](https://github.com/sentiric/sentiric-governance) repository for coding standards and contribution guidelines.

**License:**
This project is licensed under the [License](LICENSE).
