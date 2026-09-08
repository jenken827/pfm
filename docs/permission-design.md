# PFM 权限系统设计方案

> ⚠️ 历史文档说明：本文基于早期 Casdoor 认证 + 角色体系设计。
> 当前实现已整体改为**本地认证**（`users` 表 bcrypt + HS256 JWT，管理员走本地 `admin_users` 表），
> Casdoor 相关代码已全部移除，认证相关架构见「设计方案.md §3.2.1」。
> 下文目录级权限模型（继承/覆盖）仍然有效，Casdoor 相关内容仅作设计演进参考。

## 1. 概述

### 1.1 目标
- 管理员（pfm_admin）拥有全部权限，无任何限制
- 普通用户（pfm_user1）只能看到和操作被分配的目录
- 目录权限支持继承：子目录默认继承父目录权限，可单独覆盖
- 权限分配由 PFM 管理员在应用内完成

### 1.2 职责划分

| 组件 | 职责 |
|------|------|
| **Casdoor** | 用户认证 + 角色判定（admin / user） |
| **PFM 后端** | 目录级权限存储、检查、继承计算 |
| **PFM 前端** | 权限管理界面（管理员）、权限受限的 UI（普通用户） |

## 2. 角色模型（Casdoor 侧）

### 2.1 角色定义

在 Casdoor 的 `org_jk` 组织下创建两个角色：

| 角色名 | 显示名 | 说明 |
|--------|--------|------|
| `pfm_admin` | PFM管理员 | 拥有应用全部权限 |
| `pfm_user` | PFM普通用户 | 受目录权限约束 |

### 2.2 用户分配

| 用户 | 角色 |
|------|------|
| `pfm_admin@0259.eu.org` | `pfm_admin` |
| `pfm_user1@<域名>` | `pfm_user` |

### 2.3 应用限制

在 Casdoor 的 `app_pfm` 应用配置中，将 `pfm_admin` 和 `pfm_user1` 加入应用的用户列表，确保这两个用户只能登录 PFM 应用。

### 2.4 角色获取方式

PFM 后端从 JWT token 的 `claims` 中获取用户角色：
```go
claims, _ := verifier.Verify(token)
roles := claims.User.Roles  // ["pfm_admin"] 或 ["pfm_user"]
isAdmin := contains(roles, "pfm_admin")
```

## 3. 目录权限模型（PFM 侧）

### 3.1 权限类型（位掩码）

```go
const (
    PermRead     uint32 = 1 << 0  // 0001 - 查看/下载文件
    PermUpload   uint32 = 1 << 1  // 0010 - 上传新文件
    PermMkdir    uint32 = 1 << 2  // 0100 - 创建子目录
    PermDelete   uint32 = 1 << 3  // 1000 - 删除文件/子目录
    PermMove     uint32 = 1 << 4  // 10000 - 移动文件/目录
    PermRename   uint32 = 1 << 5  // 100000 - 重命名文件/目录

    PermAll      uint32 = PermRead | PermUpload | PermMkdir | PermDelete | PermMove | PermRename
    PermReadOnly uint32 = PermRead
    PermDefault  uint32 = PermRead | PermUpload | PermMkdir  // 常用默认
)
```

### 3.2 数据库表

#### directory_permission 表

```sql
CREATE TABLE directory_permission (
    id          BIGSERIAL PRIMARY KEY,
    user_id     VARCHAR(255) NOT NULL,    -- Casdoor 用户名（如 "pfm_user1"）
    file_id     CHAR(36) NOT NULL,        -- 目录 ID（关联 files.id）
    permissions INT NOT NULL DEFAULT 0,   -- 位掩码
    inherit     BOOLEAN NOT NULL DEFAULT TRUE,  -- 子目录是否继承此权限
    created_at  TIMESTAMP DEFAULT NOW(),
    updated_at  TIMESTAMP DEFAULT NOW(),
    created_by  VARCHAR(255),             -- 操作者（管理员用户名）
    UNIQUE(user_id, file_id)
);

CREATE INDEX idx_dp_user_id ON directory_permission(user_id);
CREATE INDEX idx_dp_file_id ON directory_permission(file_id);
```

#### permission_audit_log 表

```sql
CREATE TABLE permission_audit_log (
    id          BIGSERIAL PRIMARY KEY,
    operator    VARCHAR(255) NOT NULL,    -- 操作者
    target_user VARCHAR(255) NOT NULL,    -- 被操作的用户
    file_id     CHAR(36) NOT NULL,        -- 目录 ID
    action      VARCHAR(50) NOT NULL,     -- GRANT / REVOKE / UPDATE
    old_perms   INT,                      -- 原权限值
    new_perms   INT,                      -- 新权限值
    created_at  TIMESTAMP DEFAULT NOW()
);
```

### 3.3 权限继承规则

```
规则1：管理员角色 → 跳过所有权限检查，直接放行
规则2：用户在目录 X 有显式权限 → 使用该权限
规则3：目录 X 无显式权限 → 沿 parent_id 向上查找最近的祖先目录权限
规则4：找到祖先权限时，检查 inherit=true → 继承该权限
规则5：遍历到根目录仍无权限 → 拒绝访问
```

**继承链示例：**
```
/ (根目录) ← 无权限分配
├── photos/ ← 分配给 user1: Read=1, inherit=true
│   ├── 2024/ ← 无显式权限 → 继承 photos/ 的权限 (Read=1)
│   │   └── trip.jpg ← 继承 2024/ 的权限
│   └── private/ ← 显式分配: 无权限 (permissions=0) → 覆盖，拒绝访问
└── docs/ ← 分配给 user1: Read|Upload|Mkdir|Delete, inherit=true
    └── work/ ← 继承 docs/ 的权限
```

### 3.4 目录可见性规则

```
管理员：看到所有目录和文件
普通用户：
  - 根目录列表：只显示有显式权限分配的目录
  - 进入某目录后：显示该目录下的所有子项，但子目录如果无权限则显示为不可进入
  - 文件列表：只显示用户有 PermRead 权限的目录下的文件
```

**实现方式：**
- `GetFolderList`：管理员返回全部；普通用户只返回其有权限的顶层目录
- `GetFileList(parentId)`：先检查用户对 parentId 的权限；返回子项时标记每个子目录的权限
- 无权限的子目录在前端显示为「锁定」状态（图标变灰，点击提示无权限）

## 4. 权限检查流程

### 4.1 后端中间件

```
请求进入 → AuthInterceptor（认证） → PermInterceptor（授权） → 业务逻辑
```

**PermInterceptor 逻辑：**
1. 从 context 获取 claims
2. 检查用户角色：admin → 跳过，直接放行
3. 从请求参数提取 file_id（或 parent_id）
4. 调用 CheckPermission(userId, fileId, requiredPerm)
5. 无权限 → 返回 403

### 4.2 CheckPermission 算法

```go
func CheckPermission(userId string, fileId string, requiredPerm uint32) bool {
    currentId := fileId
    for currentId != "0" && currentId != "" {
        perm, err := getDirectoryPermission(userId, currentId)
        if err == nil {
            if perm.Permissions & requiredPerm == requiredPerm {
                return true  // 有权限
            }
            return false  // 显式拒绝或权限不足
        }
        // 无显式权限，检查继承
        parent := getParentId(currentId)
        if parent == "" {
            break
        }
        // 检查父目录是否有 inherit=true 的权限
        parentPerm, err := getDirectoryPermission(userId, parent)
        if err == nil && parentPerm.Inherit {
            return parentPerm.Permissions & requiredPerm == requiredPerm
        }
        currentId = parent
    }
    return false  // 无任何权限
}
```

### 4.3 需要权限检查的 API

| API | 所需权限 |
|-----|---------|
| GetFileList, GetFolderList | PermRead |
| GetFileDetail, GetFileInfo | PermRead |
| CreateDir | PermMkdir |
| Upload（小文件/分片上传） | PermUpload |
| DeleteFile | PermDelete |
| RenameFile | PermRename |
| MoveFile | PermMove |
| Download / Preview | PermRead |
| RestoreFromTrash | PermDelete |
| DeletePermanently | PermDelete |

**豁免 API（管理员或公共）：**
- SearchFiles（管理员全局搜索；普通用户只搜索有权限的目录）
- GetRecentFiles（只返回有权限的文件）
- GetTrashList（管理员全部；普通用户只看自己有权限的）
- 共享链接相关（管理员操作）
- 权限管理 API（仅管理员）

## 5. Proto 变更

### 5.1 新增 PermissionService

```protobuf
service PermissionService {
    rpc GrantDirectoryPermission(GrantDirectoryPermissionReq) returns (BaseResp);
    rpc RevokeDirectoryPermission(RevokeDirectoryPermissionReq) returns (BaseResp);
    rpc UpdateDirectoryPermission(UpdateDirectoryPermissionReq) returns (BaseResp);
    rpc GetUserPermissions(GetUserPermissionsReq) returns (BaseResp);
    rpc GetDirectoryPermissions(GetDirectoryPermissionsReq) returns (BaseResp);
    rpc ListPermissionUsers(ListPermissionUsersReq) returns (BaseResp);
}
```

### 5.2 FileService 返回值变更

`GetFileList` 和 `GetFolderList` 的响应中增加权限信息：
```protobuf
message FileListItem {
    // 现有字段...
    uint32 my_permissions = 20;  // 当前用户对该文件的权限位掩码
    bool is_locked = 21;         // 无权限的子目录标记
}
```

## 6. 前端变更

### 6.1 管理员界面

- 新增「权限管理」页面
  - 用户列表（显示角色）
  - 点击用户 → 显示已分配的目录权限列表
  - 支持「分配目录」→ 选择目录树 → 设置权限组合
  - 支持「批量分配」和「撤销」

### 6.2 普通用户界面

- 目录树只显示有权限的目录
- 无权限的子目录显示锁定图标
- 上传/删除/重命名按钮根据权限位掩码动态显示/隐藏
- 权限不足时操作按钮置灰，tooltip 提示

## 7. 实施计划

### Phase 1：Casdoor 配置（无代码变更）
- 创建角色 pfm_admin / pfm_user
- 分配用户到角色
- 限制应用访问

### Phase 2：后端数据层
- 新增 directory_permission 表
- 新增 permission_audit_log 表
- 实现 db 层 CRUD

### Phase 3：后端服务层
- 实现 PermissionService RPC
- 实现 CheckPermission 函数
- 实现权限继承逻辑

### Phase 4：后端中间件
- 实现 PermInterceptor
- 修改现有 FileService 方法加入权限检查
- 修改 GetFileList/GetFolderList 返回权限信息

### Phase 5：前端
- 管理员权限管理界面
- 普通用户的权限受限 UI

### Phase 6：文档
- 更新设计方案.md
- 更新 method_index.md
- 创建权限系统使用指南

## 8. 待确认问题

1. **Casdoor 管理员密码**：需要你提供，以便通过 API 创建角色和分配用户
2. **权限继承的默认值**：新分配的目录权限默认 inherit=true，是否合适？
3. **搜索功能**：普通用户是否应该搜索所有有权限的文件（跨目录），还是只搜索当前目录？
4. **WebDAV 访问**：WebDAV 是否也需要权限控制？
5. **共享链接**：管理员创建的共享链接，普通用户是否也能创建？
