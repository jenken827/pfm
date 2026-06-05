#!/usr/bin/env python3
"""Casdoor 角色配置脚本 - 在本地运行，不会暴露密码"""
import json
import os
import sys
import urllib.request

CASDOOR_URL = "http://localhost:8000"
ADMIN_USER = "admin"
ORG = "built-in"

def api(method, path, token=None, data=None):
    url = f"{CASDOOR_URL}{path}"
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    body = json.dumps(data).encode() if data else None
    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read())
    except Exception as e:
        return {"status": "error", "msg": str(e)}

def main():
    print("=== Casdoor 角色配置脚本 ===\n")
    
    # 1. 获取 admin token
    password = os.environ.get("CASDOOR_ADMIN_PASS")
    if not password:
        print("❌ 请设置环境变量 CASDOOR_ADMIN_PASS")
        print("   CASDOOR_ADMIN_PASS=xxx python3 scripts/casdoor_setup.py")
        sys.exit(1)
    print("\n[1/4] 获取管理员 token...")
    resp = api("POST", "/api/login", data={
        "type": "code",
        "username": ADMIN_USER,
        "password": password,
        "organization": ORG,
        "application": "app-built-in"
    })
    token = resp.get("data")
    if not token or token == "None":
        print(f"❌ 获取 token 失败: {resp}")
        sys.exit(1)
    print("✅ Token 获取成功")
    
    # 2. 创建角色
    roles = [
        {"owner": "admin", "name": "pfm_admin", "displayName": "PFM管理员",
         "description": "拥有PFM应用全部权限", "isEnabled": True},
        {"owner": "admin", "name": "pfm_user", "displayName": "PFM普通用户",
         "description": "受目录权限约束的普通用户", "isEnabled": True},
    ]
    
    for i, role in enumerate(roles):
        print(f"\n[2/4] 创建角色 {role['name']}...")
        resp = api("POST", "/api/add-role", token=token, data=role)
        if resp.get("data") == "Affected":
            print(f"✅ 创建成功")
        elif "already exists" in str(resp.get("msg", "")):
            print(f"⚠️ 已存在，跳过")
        else:
            print(f"⚠️ {resp}")
    
    # 3. 分配用户
    assignments = [
        ("pfm_admin", "pfm_admin@0259.eu.org"),
        ("pfm_user", "pfm_user1@<域名>"),
    ]
    
    for role_name, user_name in assignments:
        print(f"\n[3/4] 分配 {user_name} -> {role_name}...")
        # 获取角色
        resp = api("GET", f"/api/get-role?id=admin/{role_name}", token=token)
        role = resp.get("data", {})
        if not role:
            print(f"❌ 获取角色失败: {resp}")
            continue
        
        users = role.get("users") or []
        if user_name in users:
            print(f"⚠️ 已存在，跳过")
            continue
        
        users.append(user_name)
        role["users"] = users
        resp = api("POST", "/api/update-role", token=token, data=role)
        if resp.get("data") == "Affected":
            print(f"✅ 已分配")
        else:
            print(f"⚠️ {resp}")
    
    # 4. 验证
    print("\n=== 验证 ===")
    resp = api("GET", "/api/get-roles", token=token)
    roles_data = resp.get("data", [])
    if roles_data:
        for r in roles_data:
            users = r.get("users") or []
            print(f"  {r['name']} ({r.get('displayName', '')}) users={users}")
    else:
        print(f"  获取角色列表失败: {resp}")
    
    print("\n完成！")

if __name__ == "__main__":
    main()
