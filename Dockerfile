FROM python:3.10-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
# YENİ CMD: python'u "-u" (unbuffered) modunda çalıştırıyoruz.
# Bu, tüm 'print' çıktılarının anında loglara yazdırılmasını garanti eder.
CMD ["python", "-u", "main.py"]