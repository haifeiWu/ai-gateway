#!/bin/bash
# AI Gateway 端到端验证脚本
# 用法: ./scripts/verify.sh [base_url] [admin_token]
# 默认: base_url=http://localhost:8080, admin_token 从 .env 读取

set -euo pipefail

# 绕过本地代理，避免 curl 请求被转发到代理服务器
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY no_proxy NO_PROXY

BASE="${1:-http://localhost:8080}"
ADMIN="${2:-$(grep ADMIN_TOKEN .env 2>/dev/null | cut -d= -f2)}"
AUTH=(-H "Authorization: Bearer $ADMIN" -H "Content-Type: application/json")

red()   { echo -e "\033[31m$*\033[0m"; }
green() { echo -e "\033[32m$*\033[0m"; }

check() {
  local label="$1" expected="$2" actual="$3"
  if [ "$actual" = "$expected" ]; then
    green "  ✔ $label"
  else
    red "  ✘ $label (expected: $expected, got: $actual)"
    exit 1
  fi
}

echo "========================================"
echo " AI Gateway 端到端验证"
echo " Base: $BASE"
echo "========================================"

# ---- 1. 健康检查 ----
echo ""
echo "[1/8] 健康检查"
STATUS=$(curl -s "$BASE/health" | jq -r .status)
check "health status" "ok" "$STATUS"

# ---- 2. 创建租户 ----
echo ""
echo "[2/8] 创建租户"
TID=$(curl -s "${AUTH[@]}" -X POST "$BASE/admin/v1/tenants" \
  -d '{"name":"e2e-verify-tenant"}' | jq -r .id)
if [ -n "$TID" ] && [ "$TID" != "null" ]; then
  green "  ✔ tenant id: $TID"
else
  red "  ✘ 创建租户失败"
  exit 1
fi

# ---- 3. 创建 Key ----
echo ""
echo "[3/8] 创建 API Key"
RESP=$(curl -s "${AUTH[@]}" -X POST "$BASE/admin/v1/tenants/$TID/keys" \
  -d '{"name":"e2e-verify-key","scopes":{}}')
KEY=$(echo "$RESP" | jq -r .key)
check "key prefix" "sk-agw-" "$(echo "$KEY" | cut -c1-7)"
check "key length (39)" "39" "${#KEY}"

# ---- 4. Chat Completions 代理 ----
echo ""
echo "[4/8] Chat Completions 代理"
OBJ=$(curl -s "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}' | jq -r .object)
check "chat response object" "chat.completion" "$OBJ"

# ---- 5. Embeddings 代理 ----
echo ""
echo "[5/8] Embeddings 代理"
OBJ=$(curl -s "$BASE/v1/embeddings" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"text-embedding-ada-002","input":"test"}' | jq -r .object)
check "embed response object" "list" "$OBJ"

# ---- 6. Models 代理 ----
echo ""
echo "[6/8] Models 代理"
OBJ=$(curl -s "$BASE/v1/models" \
  -H "Authorization: Bearer $KEY" | jq -r .object)
check "models response object" "list" "$OBJ"

# ---- 7. 无效 / 过期 / 越权 ----
echo ""
echo "[7/8] 鉴权拒绝"

# 7a: 无效 Key
ERR=$(curl -s "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer sk-agw-invalid000000000000000000000" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}' | jq -r .error.type)
check "invalid key → 401" "auth_error" "$ERR"

# 7b: 过期 Key
EXP_KEY=$(curl -s "${AUTH[@]}" -X POST "$BASE/admin/v1/tenants/$TID/keys" \
  -d '{"name":"expired","scopes":{},"expires_at":"2020-01-01T00:00:00Z"}' | jq -r .key)
MSG=$(curl -s "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $EXP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}' | jq -r .error.message)
check "expired key → 401" "API key expired" "$MSG"

# 7c: 越权模型
SCOPED_KEY=$(curl -s "${AUTH[@]}" -X POST "$BASE/admin/v1/tenants/$TID/keys" \
  -d '{"name":"scoped","scopes":{"allowed_models":["gpt-4"]}}' | jq -r .key)
ERR=$(curl -s "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $SCOPED_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-3.5-turbo","messages":[{"role":"user","content":"hi"}]}' | jq -r .error.type)
check "disallowed model → 403" "forbidden" "$ERR"

# ---- 8. 清理 ----
echo ""
echo "[8/8] 清理"
curl -s "${AUTH[@]}" -X DELETE "$BASE/admin/v1/tenants/$TID" > /dev/null
green "  ✔ tenant deleted"

echo ""
echo "========================================"
echo " 全部 8 项验证通过 ✔"
echo "========================================"
