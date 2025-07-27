FROM python:3.10-slim

WORKDIR /app

COPY requirements.txt .

# YENİ EKLENEN ADIM:
# 'pip'in git repolarını klonlayabilmesi için önce 'git'i kuruyoruz.
# '--no-install-recommends' ve 'rm -rf' komutları, imaj boyutunu
# küçük tutmak için gereksiz dosyaları temizler.
RUN apt-get update && \
    apt-get install -y git --no-install-recommends && \
    rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir -r requirements.txt

COPY . .

# python'u "-u" (unbuffered) modunda çalıştırıyoruz.
CMD ["python", "-u", "main.py"]