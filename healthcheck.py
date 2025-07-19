import sys
import http.client

try:
    conn = http.client.HTTPConnection("localhost", 8000, timeout=5)
    conn.request("GET", "/health")
    response = conn.getresponse()
    if response.status == 200:
        print("Healthcheck successful!")
        sys.exit(0) # Başarılı
    else:
        print(f"Healthcheck failed with status: {response.status}")
        sys.exit(1) # Başarısız
except Exception as e:
    print(f"Healthcheck connection error: {e}")
    sys.exit(1) # Başarısız