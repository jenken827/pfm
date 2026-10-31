package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/utils"
)

// mcpTokenNameMax 令牌标签长度上限（与表里的 size:64 一致）
const mcpTokenNameMax = 64

// mcpTokenExpiresDaysMax 有效期上限（天）：再长就没有"可撤销"的意义了
const mcpTokenExpiresDaysMax = 3650

// CreateMcpToken 签发 MCP 令牌（明文只在返回值里出现一次）
//
// 归属固定取当前登录用户；普通用户给自己签，管理员也走同一条路（不给别人代签——
// 令牌等于长期凭据，代签等于替别人保管密码）。根目录必须是自己名下的目录。
func CreateMcpToken(ctx context.Context, req *native.CreateMcpTokenReq) (*native.CreateMcpTokenResp, *models.Err) {
	ownerID := getCurrentUserIDFromCtx(ctx)
	if ownerID == "" {
		return nil, unauthorizedErr()
	}
	name, err := mcpTokenName(req.GetName())
	if err != nil {
		return nil, err
	}
	rootID, err := validateMcpTokenRoot(ctx, ownerID, req.GetRootFolderId())
	if err != nil {
		return nil, err
	}
	plain, hash, genErr := auth.GenerateMcpToken()
	if genErr != nil {
		return nil, &models.Err{E: genErr, C: native.RespCode_INTERNAL_ERROR}
	}

	row := &entity.McpTokenORM{
		OwnerId:      proto.String(ownerID),
		Name:         proto.String(name),
		TokenHash:    proto.String(hash),
		AllowRead:    proto.Bool(true),
		AllowWrite:   proto.Bool(req.GetAllowWrite()),
		RootFolderId: proto.String(rootID),
	}
	if days := req.GetExpiresInDays(); days > 0 {
		if days > mcpTokenExpiresDaysMax {
			days = mcpTokenExpiresDaysMax
		}
		expiresAt := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		row.ExpiresAt = &expiresAt
	}
	if createErr := db.CreateMcpToken(row); createErr != nil {
		return nil, &models.Err{E: createErr, C: native.RespCode_INTERNAL_ERROR}
	}

	infos, infoErr := buildMcpTokenInfos([]*entity.McpTokenORM{row})
	if infoErr != nil {
		return nil, infoErr
	}
	resp := &native.CreateMcpTokenResp{Plaintext: proto.String(plain)}
	if len(infos) == 1 {
		resp.Token = infos[0]
	}
	return resp, nil
}

// ListMcpTokens 列出令牌：普通用户只看到自己的，管理员可以指定 owner_id 查别人
func ListMcpTokens(ctx context.Context, req *native.ListMcpTokensReq) (*native.ListMcpTokensResp, *models.Err) {
	ownerID := getCurrentUserIDFromCtx(ctx)
	if ownerID == "" {
		return nil, unauthorizedErr()
	}
	target := strings.TrimSpace(req.GetOwnerId())
	if target != "" && target != ownerID {
		if !isAdminUser(ctx) {
			return nil, &models.Err{E: fmt.Errorf("只能查看自己的令牌"), C: native.RespCode_PERMISSION_DENIED}
		}
		ownerID = target
	}
	rows, err := db.ListMcpTokens(ownerID)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	infos, infoErr := buildMcpTokenInfos(rows)
	if infoErr != nil {
		return nil, infoErr
	}
	return &native.ListMcpTokensResp{Tokens: infos}, nil
}

// RevokeMcpToken 撤销令牌（删行即失效，附带的行不存在就报 NOT_FOUND）
func RevokeMcpToken(ctx context.Context, req *native.RevokeMcpTokenReq) *models.Err {
	ownerID := getCurrentUserIDFromCtx(ctx)
	if ownerID == "" {
		return unauthorizedErr()
	}
	if req.GetId() == 0 {
		return &models.Err{E: fmt.Errorf("缺少令牌 id"), C: native.RespCode_PARAMS_ERROR}
	}
	row, err := db.GetMcpTokenByID(req.GetId())
	if err != nil {
		return &models.Err{E: fmt.Errorf("令牌不存在"), C: native.RespCode_NOT_FOUND}
	}
	if ownerID != utils.DerefString(row.OwnerId) && !isAdminUser(ctx) {
		return &models.Err{E: fmt.Errorf("只能撤销自己的令牌"), C: native.RespCode_PERMISSION_DENIED}
	}
	if _, delErr := db.DeleteMcpToken(req.GetId()); delErr != nil {
		return &models.Err{E: delErr, C: native.RespCode_INTERNAL_ERROR}
	}
	return nil
}

// mcpTokenName 校验标签（空标签给个默认值，便于列表里认人）
func mcpTokenName(raw string) (string, *models.Err) {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = "未命名"
	}
	if len([]rune(name)) > mcpTokenNameMax {
		return "", &models.Err{E: fmt.Errorf("标签最长 %d 个字符", mcpTokenNameMax), C: native.RespCode_PARAMS_ERROR}
	}
	return name, nil
}

// validateMcpTokenRoot 校验可见根目录：必须存在、是目录、且属于当前用户
func validateMcpTokenRoot(ctx context.Context, ownerID, raw string) (string, *models.Err) {
	rootID := strings.TrimSpace(raw)
	if rootID == "" {
		return "", nil
	}
	file, err := db.GetFileByID(rootID)
	if err != nil {
		return "", &models.Err{E: fmt.Errorf("根目录不存在"), C: native.RespCode_NOT_FOUND}
	}
	if utils.DerefString(file.OwnerId) != ownerID {
		return "", &models.Err{E: fmt.Errorf("只能用自己的目录做可见范围"), C: native.RespCode_PERMISSION_DENIED}
	}
	if file.Type != nil && *file.Type != int32(native.FileType_FOLDER) {
		return "", &models.Err{E: fmt.Errorf("可见范围必须是目录"), C: native.RespCode_PARAMS_ERROR}
	}
	return rootID, nil
}

// timestampOrNil *time.Time → *timestamppb.Timestamp（nil 保持 nil）
func timestampOrNil(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}

// buildMcpTokenInfos ORM → 对外视图（不暴露 token_hash）
func buildMcpTokenInfos(rows []*entity.McpTokenORM) ([]*native.McpTokenInfo, *models.Err) {
	if len(rows) == 0 {
		return nil, nil
	}
	paths, pathErr := folderPathMap(rows)
	if pathErr != nil {
		return nil, pathErr
	}
	names := ownerNames(rows)
	now := time.Now()
	infos := make([]*native.McpTokenInfo, 0, len(rows))
	for _, row := range rows {
		ownerID := utils.DerefString(row.OwnerId)
		rootID := utils.DerefString(row.RootFolderId)
		info := &native.McpTokenInfo{
			Id:           row.Id,
			Name:         proto.String(utils.DerefString(row.Name)),
			OwnerId:      proto.String(ownerID),
			AllowRead:    proto.Bool(utils.DerefBool(row.AllowRead)),
			AllowWrite:   proto.Bool(utils.DerefBool(row.AllowWrite)),
			RootFolderId: proto.String(rootID),
			CreatedAt:    timestampOrNil(row.CreatedAt),
			ExpiresAt:    timestampOrNil(row.ExpiresAt),
			LastUsedAt:   timestampOrNil(row.LastUsedAt),
			Expired:      proto.Bool(row.ExpiresAt != nil && row.ExpiresAt.Before(now)),
		}
		if name, ok := names[ownerID]; ok {
			info.OwnerName = proto.String(name)
		}
		if path, ok := paths[rootID]; ok {
			info.RootFolderPath = proto.String(path)
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// folderPathMap 批量把根目录 id 解析成可读路径（令牌数量很少，逐层上溯足够）
func folderPathMap(rows []*entity.McpTokenORM) (map[string]string, *models.Err) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id := utils.DerefString(row.RootFolderId); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	files, err := db.GetFilesByIDs(ids)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	known := make(map[string]*entity.FileORM, len(files))
	for _, file := range files {
		known[utils.DerefString(file.Id)] = file
	}
	paths := make(map[string]string, len(ids))
	for _, id := range ids {
		paths[id] = buildFolderPath(id, known)
	}
	return paths, nil
}

// buildFolderPath 从目录 id 上溯拼出 /a/b/c 形状的路径
func buildFolderPath(id string, known map[string]*entity.FileORM) string {
	var segments []string
	current := id
	for depth := 0; depth < 64; depth++ {
		file, ok := known[current]
		if !ok {
			var err error
			file, err = db.GetFileByID(current)
			if err != nil {
				break
			}
			known[current] = file
		}
		segments = append([]string{utils.DerefString(file.Name)}, segments...)
		parent := utils.DerefString(file.ParentId)
		if parent == "" {
			break
		}
		current = parent
	}
	if len(segments) == 0 {
		return ""
	}
	return "/" + strings.Join(segments, "/")
}

// ownerNames 取令牌归属用户名（列表里展示"谁签的"；查不到就留空）
func ownerNames(rows []*entity.McpTokenORM) map[string]string {
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		ownerID := utils.DerefString(row.OwnerId)
		if ownerID == "" {
			continue
		}
		if _, done := names[ownerID]; done {
			continue
		}
		user, err := db.GetUserByID(ownerID)
		if err != nil {
			continue
		}
		names[ownerID] = utils.DerefString(user.Username)
	}
	return names
}

// unauthorizedErr 未登录
func unauthorizedErr() *models.Err {
	return &models.Err{E: fmt.Errorf("未登录"), C: native.RespCode_TOKEN_INVALID}
}
