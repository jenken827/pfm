#!/bin/bash
# Casdoor 角色配置脚本
# 用法: bash scripts/casdoor_roles.sh
# 会提示输入 Casdoor 管理员密码

set -e

CASDOOR_URL="http://localhost:8000"
ADMIN_USER="admin"
ORG="built-in"

echo "=== Casdoor 角色配置脚本 ==="
echo ""
read -sp "请输入 Casdoor 管理员密码: " ADMIN_PASS
echo ""

# 1. 获取 admin token
echo ""
echo "[1/4] 获取管理员 token..."
TOKEN_RESP=$(curl -s -X POST "$CASDOOR_URL/api/login" \
  -H "Content-Type: application/json" \
  -d "{\"type\":\"code\",\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\",\"organization\":\"$ORG\",\"application\":\"app-built-in\"}")

TOKEN=$(echo "$TOKEN_RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('data',''))" 2>/dev/null)

if [ -z "$TOKEN" ] || [ "$TOKEN" = "None" ]; then
    echo "❌ 获取 token 失败，请检查密码"
    echo "响应: $TOKEN_RESP"
    exit 1
fi
echo "✅ Token 获取成功"

# 2. 创建 pfm_admin 角色
echo ""
echo "[2/4] 创建 pfm_admin 角色..."
curl -s -X POST "$CASDOOR_URL/api/add-role" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"owner":"admin","name":"pfm_admin","displayName":"PFM管理员","description":"拥有PFM应用全部权限","isEnabled":true}' | python3 -c "
import sys,json
d=json.load(sys.stdin)
if d.get('data')=='Affected': print('✅ 创建成功')
elif 'already exists' in str(d.get('msg','')): print('⚠️ 已存在')
else: print(f'⚠️ {d}')
" 2>/dev/null

# 3. 创建 pfm_user 角色
echo ""
echo "[3/4] 创建 pfm_user 角色..."
curl -s -X POST "$CASDOOR_URL/api/add-role" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"owner":"admin","name":"pfm_user","displayName":"PFM普通用户","description":"受目录权限约束的普通用户","isEnabled":true}' | python3 -c "
import sys,json
d=json.load(sys.stdin)
if d.get('data')=='Affected': print('✅ 创建成功')
elif 'already exists' in str(d.get('msg','')): print('⚠️ 已存在')
else: print(f'⚠️ {d}')
" 2>/dev/null

# 4. 分配用户到角色
echo ""
echo "[4/4] 分配用户到角色..."

# pfm_admin@0259.eu.org -> pfm_admin
echo "  分配 pfm_admin@0259.eu.org -> pfm_admin..."
# 先获取当前角色
ROLE_RESP=$(curl -s "$CASDOOR_URL/api/get-role?id=admin/pfm_admin" -H "Authorization: Bearer $TOKEN")
ROLE_DATA=$(echo "$ROLE_RESP" | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin).get('data',{})))" 2>/dev/null)
# 添加用户
echo "$ROLE_DATA" | python3 -c "
import sys,json
role = json.loads(sys.stdin.read())
users = role.get('users') or []
if 'pfm_admin@0259.eu.org' not in users:
    users.append('pfm_admin@0259.eu.org')
    role['users'] = users
    print(json.dumps(role))
else:
    print('SKIP')
" 2>/dev/null | while read -r line; do
    if [ "$line" = "SKIP" ]; then
        echo "  ⚠️ 已存在"
    else
        curl -s -X POST "$CASDOOR_URL/api/update-role" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$line" | python3 -c "import sys,json; d=json.load(sys.stdin); print('  ✅ 已分配' if d.get('data')=='Affected' else f'  ⚠️ {d}')" 2>/dev/null
    fi
done

# pfm_user1@<域名> -> pfm_user
echo "  分配 pfm_user1@<域名> -> pfm_user..."
ROLE_RESP=$(curl -s "$CASDOOR_URL/api/get-role?id=admin/pfm_user" -H "Authorization: Bearer $TOKEN")
ROLE_DATA=$(echo "$ROLE_RESP" | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin).get('data',{})))" 2>/dev/null)
echo "$ROLE_DATA" | python3 -c "
import sys,json
role = json.loads(sys.stdin.read())
users = role.get('users') or []
if 'pfm_user1@<域名>' not in users:
    users.append('pfm_user1@<域名>')
    role['users'] = users
    print(json.dumps(role))
else:
    print('SKIP')
" 2>/dev/null | while read -r line; do
    if [ "$line" = "SKIP" ]; then
        echo "  ⚠️ 已存在"
    else
        curl -s -X POST "$CASDOOR_URL/api/update-role" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$line" | python3 -c "import sys,json; d=json.load(sys.stdin); print('  ✅ 已分配' if d.get('data')=='Affected' else f'  ⚠️ {d}')" 2>/dev/null
    fi
done

echo ""
echo "=== 验证 ==="
echo "角色列表:"
curl -s "$CASDOOR_URL/api/get-roles" -H "Authorization: Bearer $TOKEN" | python3 -c "
import sys,json
data = json.load(sys.stdin).get('data',[])
for r in data:
    print(f\"  {r['name']} ({r.get('displayName','')}) users={r.get('users',[])}\")
" 2>/dev/null
echo ""
echo "完成！"
