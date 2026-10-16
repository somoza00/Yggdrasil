# Container one-shot do e2e (compose profile "e2e"). Ver tests/e2e/test_full_flow.py.
FROM python:3.13-slim
WORKDIR /e2e
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY test_full_flow.py .
USER 10001
CMD ["pytest", "-q", "-p", "no:cacheprovider", "test_full_flow.py"]
