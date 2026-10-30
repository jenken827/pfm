package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/models"
	"github.com/jenken827/pfm/pkg/llog"
	"github.com/jenken827/pfm/utils"
	"gorm.io/gorm"
)

// WebDAV 独立账号（与登录账号完全解耦）。
//
// 设计要点：
//   - 凭据独立：username 是 /dav 专用命名空间（与 users.username 无关），
//     password_hash 为 bcrypt；账号不能登录 App、不能签发 JWT。
//   - 每个账号绑定用户自己的一个目录作为「WebDAV 根」，路径解析从该根逐级向下，
//     天然无法越出该子树（见 server/webdav/tree.go 的 hostFileTree.resolve）。
//   - 只读账号在 handler 层按方法白名单拦截写操作。
//   - 认证走内存缓存：bcrypt 约 50-100ms/次，而 WebDAV 同步一次会发几百个请求，
//     逐请求 bcrypt 会打满 CPU；缓存命中后只剩 1 次主键查询 + 1 次绑定目录校验。
//   - 绑定目录被删除/回收后账号自动禁用（删除路径主动禁用 + 认证时兜底校验）。
const (
	webdavUsernameMinLen = 1
	webdavUsernameMaxLen = 64
	webdavPasswordMinLen = 12
	webdavPasswordMaxLen = 128

	// webdavAuthCacheTTL 认证缓存有效期（同一 (用户名,密码) 在该窗口内只做一次 bcrypt）
	webdavAuthCacheTTL = 10 * time.Minute
	// webdavAuthCacheMax 缓存条目上限；超出先清过期项，仍超则整体清空（可重建，不影响正确性）
	webdavAuthCacheMax = 4096

	webdavRootInvalidReason = "绑定目录已删除或移入回收站"
	webdavDefaultUserName   = "webdav"
	webdavGeneratedPwdLen   = 16
	webdavPathTimeLayout    = "2006-01-02 15:04:05"
)

// webdavUsernamePattern 用户名可用字符：字母数字与 . _ -（避免 Basic Auth 头里的
// 冒号/空格等歧义字符，也避免日志注入）
var webdavUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// webdavGeneratedPwdAlphabet 随机密码字符集（去掉易混淆字符 0/O/1/l/I）
const webdavGeneratedPwdAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// errWebdavAuthFailed 认证失败的统一对外错误（不区分「用户不存在」与「密码错误」）
var errWebdavAuthFailed = errors.New("invalid webdav credentials")

// webdavDummyBcryptHash 账号不存在时仍执行一次 bcrypt 比对，抹平响应时间差，
// 避免通过耗时探测用户名是否存在（该哈希对应随机明文，永远匹配不上）
const webdavDummyBcryptHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// ===== ORM 指针字段读取 =====

func webdavAccountID(a *entity.WebdavAccountORM) uint32 {
	if a == nil || a.Id == nil {
		return 0
	}
	return *a.Id
}

func webdavAccountRootID(a *entity.WebdavAccountORM) string {
	if a == nil || a.RootFileId == nil {
		return ""
	}
	return *a.RootFileId
}

func webdavAccountUserID(a *entity.WebdavAccountORM) string {
	if a == nil || a.UserId == nil {
		return ""
	}
	return *a.UserId
}

func webdavAccountDisabledReason(a *entity.WebdavAccountORM) string {
	if a == nil || a.DisabledReason == nil {
		return ""
	}
	return *a.DisabledReason
}

// ===== 认证缓存 =====

type webdavAuthCacheEntry struct {
	accountID uint32
	expiresAt time.Time
}

var (
	webdavAuthCacheMu sync.Mutex
	webdavAuthCache   = make(map[string]webdavAuthCacheEntry)
)

// webdavAuthCacheKey 缓存键 = sha256(username + \0 + password)（明文密码不落任何存储）
func webdavAuthCacheKey(username, password string) string {
	sum := sha256.Sum256([]byte(username + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

func getWebdavAuthCache(key string) (uint32, bool) {
	webdavAuthCacheMu.Lock()
	defer webdavAuthCacheMu.Unlock()
	entry, ok := webdavAuthCache[key]
	if !ok {
		return 0, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(webdavAuthCache, key)
		return 0, false
	}
	return entry.accountID, true
}

func putWebdavAuthCache(key string, accountID uint32) {
	webdavAuthCacheMu.Lock()
	defer webdavAuthCacheMu.Unlock()
	if len(webdavAuthCache) >= webdavAuthCacheMax {
		now := time.Now()
		for k, v := range webdavAuthCache {
			if now.After(v.expiresAt) {
				delete(webdavAuthCache, k)
			}
		}
		if len(webdavAuthCache) >= webdavAuthCacheMax {
			llog.L().Warnf("webdav auth cache full (%d entries), resetting", len(webdavAuthCache))
			clear(webdavAuthCache)
		}
	}
	webdavAuthCache[key] = webdavAuthCacheEntry{
		accountID: accountID,
		expiresAt: time.Now().Add(webdavAuthCacheTTL),
	}
}

// invalidateWebdavAuthCache 按账号清理认证缓存（改密/改绑定/启用禁用/删除后必须调用）
func invalidateWebdavAuthCache(accountID uint32) {
	if accountID == 0 {
		return
	}
	webdavAuthCacheMu.Lock()
	defer webdavAuthCacheMu.Unlock()
	for k, v := range webdavAuthCache {
		if v.accountID == accountID {
			delete(webdavAuthCache, k)
		}
	}
}

// ===== 自助接口（当前用户自己的账号）=====

// ListWebdavAccounts 列出当前用户的 WebDAV 账号
func ListWebdavAccounts(ctx context.Context) (*native.WebdavAccountListResp, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_TOKEN_INVALID}
	}

	accounts, err := db.ListWebdavAccountsByUser(userID)
	if err != nil {
		llog.C(ctx).Errorf("list webdav accounts failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	rootID := currentUserRootFileID(userID)
	validRoots := loadValidRootIDs(accounts, userID)
	resolver := db.NewFilePathResolver()

	items := make([]*native.WebdavAccountInfo, 0, len(accounts))
	for _, a := range accounts {
		items = append(items, buildWebdavAccountInfo(resolver, a, rootID, validRoots))
	}

	return &native.WebdavAccountListResp{
		Accounts: items,
		DavUrl:   utils.StringPtr("/dav"),
	}, nil
}

// CreateWebdavAccount 创建 WebDAV 账号
// 返回的明文密码仅本次响应可见（服务端只存 bcrypt），前端需引导用户立即复制
func CreateWebdavAccount(ctx context.Context, req *native.WebdavAccountCreateReq) (*native.WebdavAccountCreateResp, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_TOKEN_INVALID}
	}

	rootFileID := req.GetRootFileId()
	if mErr := validateBindableRootDir(userID, rootFileID); mErr != nil {
		return nil, mErr
	}

	username, mErr := resolveNewWebdavUsername(req.GetUsername())
	if mErr != nil {
		return nil, mErr
	}
	password, mErr := resolveWebdavPassword(req.GetPassword())
	if mErr != nil {
		return nil, mErr
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		llog.C(ctx).Errorf("hash webdav password failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	account := &entity.WebdavAccountORM{
		UserId:       utils.StringPtr(userID),
		Username:     utils.StringPtr(username),
		PasswordHash: utils.StringPtr(hash),
		Name:         utils.StringPtr(strings.TrimSpace(req.GetName())),
		RootFileId:   utils.StringPtr(rootFileID),
		ReadOnly:     utils.BoolPtr(req.GetReadOnly()),
		Enabled:      utils.BoolPtr(true),
	}
	if err := db.CreateWebdavAccount(account); err != nil {
		// 唯一索引兜底：并发创建同名账号时在这里被拦下
		if isWebdavUsernameTaken(err) {
			return nil, &models.Err{E: fmt.Errorf("WebDAV 用户名已存在: %s", username), C: native.RespCode_PARAMS_ERROR}
		}
		llog.C(ctx).Errorf("create webdav account failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.C(ctx).Infof("webdav account created: id=%d, user=%s, username=%s, root=%s, read_only=%v",
		webdavAccountID(account), userID, username, rootFileID, req.GetReadOnly())

	resolver := db.NewFilePathResolver()
	rootID := currentUserRootFileID(userID)
	info := buildWebdavAccountInfo(resolver, account, rootID, map[string]bool{rootFileID: true})
	return &native.WebdavAccountCreateResp{Account: info, Password: utils.StringPtr(password)}, nil
}

// UpdateWebdavAccount 更新账号（标签/绑定目录/只读/启用）
func UpdateWebdavAccount(ctx context.Context, req *native.WebdavAccountUpdateReq) *models.Err {
	userID := getCurrentUserIDFromCtx(ctx)
	if _, mErr := loadOwnWebdavAccount(userID, req.GetId()); mErr != nil {
		return mErr
	}

	fields := map[string]any{}
	if req.Name != nil {
		fields["name"] = strings.TrimSpace(req.GetName())
	}
	if req.RootFileId != nil && req.GetRootFileId() != "" {
		if bErr := validateBindableRootDir(userID, req.GetRootFileId()); bErr != nil {
			return bErr
		}
		fields["root_file_id"] = req.GetRootFileId()
	}
	if req.ReadOnly != nil {
		fields["read_only"] = req.GetReadOnly()
	}
	if req.Enabled != nil {
		fields["enabled"] = req.GetEnabled()
		// 手动启用/禁用会清掉自动禁用留下的原因
		fields["disabled_reason"] = ""
	}

	if err := db.UpdateWebdavAccountFields(req.GetId(), fields); err != nil {
		llog.C(ctx).Errorf("update webdav account %d failed: %v", req.GetId(), err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	invalidateWebdavAuthCache(req.GetId())
	llog.C(ctx).Infof("webdav account %d updated by user %s: %v", req.GetId(), userID, fields)
	return nil
}

// DeleteWebdavAccount 删除账号
func DeleteWebdavAccount(ctx context.Context, req *native.WebdavAccountDeleteReq) *models.Err {
	userID := getCurrentUserIDFromCtx(ctx)
	if _, mErr := loadOwnWebdavAccount(userID, req.GetId()); mErr != nil {
		return mErr
	}
	if err := db.DeleteWebdavAccount(req.GetId()); err != nil {
		llog.C(ctx).Errorf("delete webdav account %d failed: %v", req.GetId(), err)
		return &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	invalidateWebdavAuthCache(req.GetId())
	llog.C(ctx).Infof("webdav account %d deleted by user %s", req.GetId(), userID)
	return nil
}

// ResetWebdavAccountPassword 重置账号密码（空密码 = 服务端生成随机密码）
func ResetWebdavAccountPassword(ctx context.Context, req *native.WebdavAccountResetPasswordReq) (*native.WebdavAccountResetPasswordResp, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if _, mErr := loadOwnWebdavAccount(userID, req.GetId()); mErr != nil {
		return nil, mErr
	}

	password, mErr := resolveWebdavPassword(req.GetPassword())
	if mErr != nil {
		return nil, mErr
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		llog.C(ctx).Errorf("hash webdav password failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	if err := db.UpdateWebdavAccountPassword(req.GetId(), hash); err != nil {
		llog.C(ctx).Errorf("reset webdav account %d password failed: %v", req.GetId(), err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	invalidateWebdavAuthCache(req.GetId())
	llog.C(ctx).Infof("webdav account %d password reset by user %s", req.GetId(), userID)
	return &native.WebdavAccountResetPasswordResp{Password: utils.StringPtr(password)}, nil
}

// ===== /dav 认证入口 =====

// AuthenticateWebdav 校验 WebDAV 独立账号凭据，成功返回账号（供 handler 构造隔离上下文）。
//
// 失败时返回描述性错误（只写服务端日志，客户端一律 401）。
// 绑定目录失效时**自动禁用**账号并清缓存（决策：目录没了就禁用账号）。
func AuthenticateWebdav(ctx context.Context, username, password string) (*entity.WebdavAccountORM, error) {
	if username == "" || password == "" {
		return nil, errWebdavAuthFailed
	}

	cacheKey := webdavAuthCacheKey(username, password)
	if accountID, hit := getWebdavAuthCache(cacheKey); hit {
		account, err := db.GetWebdavAccountByID(accountID)
		if err != nil {
			invalidateWebdavAuthCache(accountID)
			return nil, fmt.Errorf("webdav account %d not found: %w", accountID, err)
		}
		if err := ensureWebdavAccountUsable(ctx, account); err != nil {
			return nil, err
		}
		return account, nil
	}

	account, err := db.GetWebdavAccountByUsername(username)
	if err != nil {
		// 账号不存在也做一次 bcrypt，抹平「用户名不存在」与「密码错误」的响应时间差
		auth.VerifyPassword(webdavDummyBcryptHash, password)
		return nil, errWebdavAuthFailed
	}
	if account.PasswordHash == nil || !auth.VerifyPassword(*account.PasswordHash, password) {
		return nil, errWebdavAuthFailed
	}
	if err := ensureWebdavAccountUsable(ctx, account); err != nil {
		return nil, err
	}

	putWebdavAuthCache(cacheKey, webdavAccountID(account))
	// 缓存未命中的一次认证 ≈ 一个客户端会话（10 分钟窗口），顺带更新最后使用时间
	if err := db.UpdateWebdavAccountLastUsed(webdavAccountID(account)); err != nil {
		llog.C(ctx).Warnf("update webdav account %d last_used_at failed: %v", webdavAccountID(account), err)
	}
	return account, nil
}

// DisableWebdavAccountsForDeletedRoots 删除文件/清空回收站后，把绑定目录已被删除的账号
// 自动禁用（事件驱动，账号列表立即可见失效状态，无需等客户端再发请求）。
// 认证路径还会再兜底校验一次（覆盖其他删除来源）。
func DisableWebdavAccountsForDeletedRoots(ctx context.Context, rootIDs []string) {
	if len(rootIDs) == 0 {
		return
	}
	ids, err := db.DisableWebdavAccountsByRootIDs(rootIDs, webdavRootInvalidReason)
	if err != nil {
		llog.C(ctx).Errorf("auto disable webdav accounts failed: %v", err)
		return
	}
	for _, id := range ids {
		invalidateWebdavAuthCache(id)
	}
	if len(ids) > 0 {
		llog.C(ctx).Warnf("auto disabled %d webdav account(s) because their root dir was deleted: %v", len(ids), ids)
	}
}

// ===== 内部辅助 =====

// ensureWebdavAccountUsable 认证通过后的可用性检查（启用状态 + 绑定目录有效性）
func ensureWebdavAccountUsable(ctx context.Context, account *entity.WebdavAccountORM) error {
	if account == nil || account.Id == nil {
		return errWebdavAuthFailed
	}
	if account.Enabled != nil && !*account.Enabled {
		return fmt.Errorf("webdav account disabled: %s", webdavAccountDisabledReason(account))
	}
	return checkWebdavRootUsable(ctx, account)
}

// checkWebdavRootUsable 校验绑定目录仍然有效（存在 + 未软删 + 属于本用户 + 是目录），
// 无效则自动禁用账号并清缓存
func checkWebdavRootUsable(ctx context.Context, account *entity.WebdavAccountORM) error {
	rootID := webdavAccountRootID(account)
	ownerID := webdavAccountUserID(account)
	if rootID == "" || ownerID == "" {
		return fmt.Errorf("webdav account %d has no root dir configured", webdavAccountID(account))
	}

	file, err := db.GetFileByID(rootID)
	usable := err == nil && file != nil &&
		file.OwnerId != nil && *file.OwnerId == ownerID &&
		file.Type != nil && *file.Type == int32(native.FileType_FOLDER)
	if usable {
		return nil
	}

	llog.C(ctx).Warnf("webdav account %d root dir %s unusable (err=%v), auto disabling", webdavAccountID(account), rootID, err)
	if account.Enabled == nil || *account.Enabled {
		if dErr := db.SetWebdavAccountEnabled(webdavAccountID(account), false, webdavRootInvalidReason); dErr != nil {
			llog.C(ctx).Errorf("auto disable webdav account %d failed: %v", webdavAccountID(account), dErr)
		}
	}
	invalidateWebdavAuthCache(webdavAccountID(account))
	return fmt.Errorf("webdav root dir unavailable: %s", webdavRootInvalidReason)
}

// loadOwnWebdavAccount 取当前用户自己的账号；不存在或不属于该用户都返回 NOT_FOUND
// （不区分，避免用错误码探测他人账号 ID）
func loadOwnWebdavAccount(userID string, id uint32) (*entity.WebdavAccountORM, *models.Err) {
	if userID == "" {
		return nil, &models.Err{E: fmt.Errorf("unauthorized"), C: native.RespCode_TOKEN_INVALID}
	}
	account, err := db.GetWebdavAccountByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &models.Err{E: fmt.Errorf("WebDAV 账号不存在"), C: native.RespCode_NOT_FOUND}
		}
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if webdavAccountUserID(account) != userID {
		return nil, &models.Err{E: fmt.Errorf("WebDAV 账号不存在"), C: native.RespCode_NOT_FOUND}
	}
	return account, nil
}

// validateBindableRootDir 校验可绑定目录：属于当前用户、是文件夹、未软删
func validateBindableRootDir(userID, fileID string) *models.Err {
	if fileID == "" {
		return &models.Err{E: fmt.Errorf("请选择 WebDAV 根目录"), C: native.RespCode_PARAMS_ERROR}
	}
	file, err := db.GetFileByID(fileID)
	if err != nil {
		return &models.Err{E: fmt.Errorf("目录不存在或无权限"), C: native.RespCode_PARAMS_ERROR}
	}
	if file.OwnerId == nil || *file.OwnerId != userID {
		return &models.Err{E: fmt.Errorf("目录不存在或无权限"), C: native.RespCode_PERMISSION_DENIED}
	}
	if file.Type == nil || *file.Type != int32(native.FileType_FOLDER) {
		return &models.Err{E: fmt.Errorf("WebDAV 根目录必须是文件夹"), C: native.RespCode_PARAMS_ERROR}
	}
	return nil
}

// resolveNewWebdavUsername 生成或校验新账号用户名
func resolveNewWebdavUsername(raw string) (string, *models.Err) {
	username := strings.TrimSpace(raw)
	if username == "" {
		generated, err := randomWebdavUsername()
		if err != nil {
			return "", &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		return generated, nil
	}
	if len(username) < webdavUsernameMinLen || len(username) > webdavUsernameMaxLen {
		return "", &models.Err{
			E: fmt.Errorf("用户名长度需在 %d-%d 之间", webdavUsernameMinLen, webdavUsernameMaxLen),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	if !webdavUsernamePattern.MatchString(username) {
		return "", &models.Err{
			E: fmt.Errorf("用户名只能包含字母、数字与 . _ -"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	if _, err := db.GetWebdavAccountByUsername(username); err == nil {
		return "", &models.Err{E: fmt.Errorf("WebDAV 用户名已存在: %s", username), C: native.RespCode_PARAMS_ERROR}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	return username, nil
}

// resolveWebdavPassword 使用请求中的密码或生成随机密码，并做长度校验
func resolveWebdavPassword(raw string) (string, *models.Err) {
	if raw == "" {
		password, err := randomWebdavPassword(webdavGeneratedPwdLen)
		if err != nil {
			return "", &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
		}
		return password, nil
	}
	if len(raw) < webdavPasswordMinLen || len(raw) > webdavPasswordMaxLen {
		return "", &models.Err{
			E: fmt.Errorf("密码长度需在 %d-%d 之间", webdavPasswordMinLen, webdavPasswordMaxLen),
			C: native.RespCode_PARAMS_ERROR,
		}
	}
	return raw, nil
}

// buildWebdavAccountInfo 组装对外视图（不含 password_hash）
func buildWebdavAccountInfo(resolver *db.FilePathResolver, account *entity.WebdavAccountORM, rootID string, validRoots map[string]bool) *native.WebdavAccountInfo {
	rootFileID := webdavAccountRootID(account)
	info := &native.WebdavAccountInfo{
		Id:             account.Id,
		Username:       account.Username,
		Name:           account.Name,
		RootFileId:     account.RootFileId,
		ReadOnly:       account.ReadOnly,
		Enabled:        account.Enabled,
		DisabledReason: account.DisabledReason,
		RootValid:      utils.BoolPtr(validRoots[rootFileID]),
	}
	if path, err := resolver.DirPath(rootFileID, rootID); err == nil {
		info.RootPath = utils.StringPtr(path)
	}
	if account.CreatedAt != nil {
		info.CreatedTime = utils.StringPtr(account.CreatedAt.Format(webdavPathTimeLayout))
	}
	if account.LastUsedAt != nil {
		info.LastUsedTime = utils.StringPtr(account.LastUsedAt.Format(webdavPathTimeLayout))
	}
	return info
}

// currentUserRootFileID 取用户空间根目录 ID（用于把展示路径收敛成相对路径）
func currentUserRootFileID(userID string) string {
	space, err := db.GetUserSpaceByUserID(userID)
	if err != nil || space.RootFileId == nil {
		return ""
	}
	return *space.RootFileId
}

// loadValidRootIDs 批量查询各账号绑定目录是否仍然有效（1 次查询，避免逐账号 N+1）
func loadValidRootIDs(accounts []*entity.WebdavAccountORM, userID string) map[string]bool {
	valid := make(map[string]bool, len(accounts))
	ids := make([]string, 0, len(accounts))
	for _, a := range accounts {
		if rootID := webdavAccountRootID(a); rootID != "" {
			ids = append(ids, rootID)
		}
	}
	if len(ids) == 0 {
		return valid
	}
	files, err := db.GetFilesByIDs(ids)
	if err != nil {
		llog.L().Warnf("load webdav account root dirs failed: %v", err)
		return valid
	}
	for _, f := range files {
		if f.Id != nil && f.OwnerId != nil && *f.OwnerId == userID &&
			f.Type != nil && *f.Type == int32(native.FileType_FOLDER) {
			valid[*f.Id] = true
		}
	}
	return valid
}

// isWebdavUsernameTaken 判断创建失败是否由用户名唯一索引冲突引起
func isWebdavUsernameTaken(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return errors.Is(err, gorm.ErrDuplicatedKey) ||
		strings.Contains(msg, "idx_wa_un") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "unique failed") ||
		strings.Contains(msg, "constraint failed: unique")
}

// randomWebdavUsername 生成默认用户名 webdav-<8 位随机>
func randomWebdavUsername() (string, error) {
	suffix, err := randomHex(4)
	if err != nil {
		return "", err
	}
	return webdavDefaultUserName + "-" + suffix, nil
}

// randomWebdavPassword 生成随机密码（crypto/rand）
func randomWebdavPassword(length int) (string, error) {
	if length <= 0 {
		length = webdavGeneratedPwdLen
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random password failed: %w", err)
	}
	out := make([]byte, length)
	for i, b := range buf {
		out[i] = webdavGeneratedPwdAlphabet[int(b)%len(webdavGeneratedPwdAlphabet)]
	}
	return string(out), nil
}

// randomHex 生成 n 字节随机数的 16 进制串
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random string failed: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
