#!/bin/bash

# OpenGate Server Test Script
# Testa os endpoints principais do servidor (modelos e chat, com e sem streaming).

PORT=${1:-8000}
URL="http://localhost:$PORT"
MODEL=${2:-opencode/big-pickle}

echo "=== Testando endpoint /v1/models ==="
curl -s "$URL/v1/models" | jq .
if [ $? -ne 0 ]; then
    echo "Erro ao conectar no servidor na porta $PORT. Ele está rodando?"
    exit 1
fi

echo -e "\n=== Testando /v1/chat/completions (sem streaming) - Modelo: $MODEL ==="
curl -s -X POST "$URL/v1/chat/completions" \
-H "Content-Type: application/json" \
-d "{
  \"model\": \"$MODEL\",
  \"messages\": [
    {\"role\": \"user\", \"content\": \"Diga Olá Mundo em poucas palavras!\"}
  ]
}" | jq .

echo -e "\n=== Testando /v1/chat/completions (com streaming) - Modelo: $MODEL ==="
curl -sN -X POST "$URL/v1/chat/completions" \
-H "Content-Type: application/json" \
-d "{
  \"model\": \"$MODEL\",
  \"stream\": true,
  \"messages\": [
    {\"role\": \"user\", \"content\": \"Conte até 3.\"}
  ]
}"

echo -e "\n\n=== Teste concluído ==="
