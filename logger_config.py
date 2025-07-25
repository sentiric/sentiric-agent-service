import logging
import sys
import structlog

def setup_logging():
    """
    Sentiric platformu için standart, yapılandırılmış JSON loglama kurar.
    """
    # Geleneksel logging kütüphanesini temel olarak yapılandır
    logging.basicConfig(
        format="%(message)s",
        stream=sys.stdout,
        level=logging.INFO,
    )
    
    # structlog'u, logları JSON formatında işleyecek şekilde yapılandır
    structlog.configure(
        processors=[
            # Log seviyesi, timestamp gibi standart bilgileri ekle
            structlog.contextvars.merge_contextvars,
            structlog.stdlib.add_logger_name,
            structlog.stdlib.add_log_level,
            structlog.processors.TimeStamper(fmt="iso"),
            # Log mesajını ve event verilerini birleştir
            structlog.processors.dict_tracebacks,
            structlog.processors.StackInfoRenderer(),
            structlog.processors.format_exc_info,
            structlog.processors.UnicodeDecoder(),
            # Nihai çıktıyı JSON olarak formatla
            structlog.processors.JSONRenderer(),
        ],
        # Çıktıyı standart logging kütüphanesine yönlendir
        wrapper_class=structlog.stdlib.BoundLogger,
        logger_factory=structlog.stdlib.LoggerFactory(),
        cache_logger_on_first_use=True,
    )

    log = structlog.get_logger("sentiric-agent-service")
    log.info("logging_setup_complete", service_name="sentiric-agent-service")
    return log