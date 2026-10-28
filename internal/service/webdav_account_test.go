package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/globals"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/dbtest"
	"github.com/jenken827/pfm/utils"
	"gorm.io/gorm"
)

// ===== 测试基建（真实 SQLite，见 internal/dbtest）=====

func webdavTestCtx(userID string) context.Context {
	return context.WithValue(context.Background(), globals.ContextUserKey,
		&auth.Claims{User: auth.User{Id: userID, Name: userID}})
}

// newTestWebdavUser 建一个用户空间 + /webdav/abc 目录树，返回 (rootID, abcID)
// userID 每次随机，避免包内多个测试互相干扰（共享同一个测试库）
func newTestWebdavUser(t *testing.T) (string, string, string) {
	t.Helper()
	dbtest.Setup(t)

	userID := uuid.New().String()
	rootID := createTestFolder(t, userID, "root-"+userID, "")
	space := &entity.UserSpaceORM{
		UserId:           utils.StringPtr(userID),
		RootFileId:       utils.StringPtr(rootID),
		BoundProviderIds: utils.StringPtr("[1]"),
		QuotaBytes:       utils.Uint64Ptr(0),
		UsedBytes:        utils.Uint64Ptr(0),
	}
	if err := db.CreateUserSpace(space); err != nil {
		t.Fatalf("create test user space failed: %v", err)
	}
	webdavDir := createTestFolder(t, userID, "webdav", rootID)
	abcID := createTestFolder(t, userID, "abc", webdavDir)
	return userID, rootID, abcID
}

func createTestFolder(t *testing.T, ownerID, name, parentID string) string {
	t.Helper()
	id := uuid.New().String()
	file := &entity.FileORM{
		Id:       utils.StringPtr(id),
		Name:     utils.StringPtr(name),
		ParentId: utils.StringPtr(parentID),
		OwnerId:  utils.StringPtr(ownerID),
		Type:     utils.EnumPtr(native.FileType_FOLDER),
		IsReady:  utils.BoolPtr(true),
	}
	if err := db.CreateFile(file); err != nil {
		t.Fatalf("create test folder %s failed: %v", name, err)
	}
	return id
}

// ===== 创建/校验 =====

func TestCreateWebdavAccountValidation(t *testing.T) {
	userID, rootID, abcID := newTestWebdavUser(t)
	ctx := webdavTestCtx(userID)

	// 绑定目录必须是文件夹：绑定用户根目录（文件夹）之外的普通文件应被拒
	fileID := uuid.New().String()
	plainFile := &entity.FileORM{
		Id:       utils.StringPtr(fileID),
		Name:     utils.StringPtr("note.txt"),
		ParentId: utils.StringPtr(rootID),
		OwnerId:  utils.StringPtr(userID),
		Type:     utils.EnumPtr(native.FileType_DOCUMENT),
		IsReady:  utils.BoolPtr(true),
	}
	if err := db.CreateFile(plainFile); err != nil {
		t.Fatalf("create test file failed: %v", err)
	}
	if _, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(fileID),
	}); mErr == nil || !strings.Contains(mErr.E.Error(), "文件夹") {
		t.Fatalf("expect '必须是文件夹' error, got %v", mErr)
	}

	// 绑定他人目录 → 拒绝
	otherUserID, _, otherAbcID := newTestWebdavUser(t)
	_ = otherUserID
	if _, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(otherAbcID),
	}); mErr == nil || mErr.C != native.RespCode_PERMISSION_DENIED {
		t.Fatalf("expect PERMISSION_DENIED for other user's dir, got %v", mErr)
	}

	// 密码过短 → 拒绝
	if _, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(abcID),
		Password:   utils.StringPtr("short"),
	}); mErr == nil || mErr.C != native.RespCode_PARAMS_ERROR {
		t.Fatalf("expect PARAMS_ERROR for short password, got %v", mErr)
	}

	// 用户名含非法字符 → 拒绝
	if _, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(abcID),
		Username:   utils.StringPtr("bad name!"),
	}); mErr == nil || mErr.C != native.RespCode_PARAMS_ERROR {
		t.Fatalf("expect PARAMS_ERROR for invalid username, got %v", mErr)
	}

	// 正常创建（用户名/密码留空 → 服务端生成）
	resp, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(abcID),
		Name:       utils.StringPtr("ReadAny"),
		ReadOnly:   utils.BoolPtr(true),
	})
	if mErr != nil {
		t.Fatalf("create webdav account failed: %v", mErr)
	}
	generatedUsername := resp.GetAccount().GetUsername()
	if !strings.HasPrefix(generatedUsername, "webdav-") {
		t.Fatalf("expect generated username with prefix webdav-, got %q", generatedUsername)
	}
	if len(resp.GetPassword()) != webdavGeneratedPwdLen {
		t.Fatalf("expect generated password length %d, got %d", webdavGeneratedPwdLen, len(resp.GetPassword()))
	}

	// 重复用户名 → 拒绝（你的新增要求）
	dup, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(abcID),
		Username:   utils.StringPtr(generatedUsername),
	})
	if dup != nil || mErr == nil || !strings.Contains(mErr.E.Error(), "已存在") {
		t.Fatalf("expect '用户名已存在' error, got %v", mErr)
	}

	// 列表：路径相对用户网盘根渲染，只读/启用/有效性回显正确
	list, mErr := ListWebdavAccounts(ctx)
	if mErr != nil {
		t.Fatalf("list webdav accounts failed: %v", mErr)
	}
	if len(list.GetAccounts()) != 1 {
		t.Fatalf("expect 1 account, got %d", len(list.GetAccounts()))
	}
	item := list.GetAccounts()[0]
	if item.GetRootPath() != "/webdav/abc" {
		t.Errorf("expect root_path /webdav/abc, got %q", item.GetRootPath())
	}
	if !item.GetReadOnly() || !item.GetEnabled() || !item.GetRootValid() {
		t.Errorf("expect read_only/enabled/root_valid all true, got %+v", item)
	}
	if list.GetDavUrl() == "" {
		t.Errorf("expect dav_url not empty")
	}
}

// ===== 认证 =====

func TestAuthenticateWebdav(t *testing.T) {
	userID, _, abcID := newTestWebdavUser(t)
	ctx := webdavTestCtx(userID)

	username := "dav-" + uuid.New().String()[:8]
	password := "correct-horse-battery"
	if _, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(abcID),
		Username:   utils.StringPtr(username),
		Password:   utils.StringPtr(password),
	}); mErr != nil {
		t.Fatalf("create webdav account failed: %v", mErr)
	}

	account, err := AuthenticateWebdav(ctx, username, password)
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}
	if account.UserId == nil || *account.UserId != userID {
		t.Fatalf("expect owner %s, got %v", userID, account.UserId)
	}

	if _, err := AuthenticateWebdav(ctx, username, password+"x"); err == nil {
		t.Errorf("expect auth failure with wrong password")
	}
	if _, err := AuthenticateWebdav(ctx, username+"nope", password); err == nil {
		t.Errorf("expect auth failure with unknown username")
	}

	// 禁用后不可认证；重新启用后恢复
	accountID := *account.Id
	if err := db.SetWebdavAccountEnabled(accountID, false, ""); err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	invalidateWebdavAuthCache(accountID)
	if _, err := AuthenticateWebdav(ctx, username, password); err == nil {
		t.Errorf("expect auth failure for disabled account")
	}
	if err := db.SetWebdavAccountEnabled(accountID, true, ""); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	invalidateWebdavAuthCache(accountID)
	if _, err := AuthenticateWebdav(ctx, username, password); err != nil {
		t.Fatalf("expect auth success after re-enable, got %v", err)
	}
}

// 绑定目录进回收站 → 认证时兜底校验自动禁用账号
func TestAuthenticateWebdavAutoDisableOnRootDeleted(t *testing.T) {
	userID, _, abcID := newTestWebdavUser(t)
	ctx := webdavTestCtx(userID)

	username := "dav-" + uuid.New().String()[:8]
	password := "correct-horse-battery"
	resp, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(abcID),
		Username:   utils.StringPtr(username),
		Password:   utils.StringPtr(password),
	})
	if mErr != nil {
		t.Fatalf("create webdav account failed: %v", mErr)
	}
	accountID := resp.GetAccount().GetId()

	// 先做一次认证让缓存生效，再软删绑定目录（不调用事件钩子，走兜底校验路径）
	if _, err := AuthenticateWebdav(ctx, username, password); err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}
	if err := db.DeleteFiles([]string{abcID}); err != nil {
		t.Fatalf("soft delete root dir failed: %v", err)
	}

	if _, err := AuthenticateWebdav(ctx, username, password); err == nil {
		t.Fatalf("expect auth failure after root dir deleted")
	}
	stored, err := db.GetWebdavAccountByID(accountID)
	if err != nil {
		t.Fatalf("load account failed: %v", err)
	}
	if stored.Enabled == nil || *stored.Enabled {
		t.Errorf("expect account auto disabled")
	}
	if stored.DisabledReason == nil || !strings.Contains(*stored.DisabledReason, "回收站") {
		t.Errorf("expect disabled_reason to mention 回收站, got %v", stored.DisabledReason)
	}
}

// 删除路径主动禁用（事件驱动）：DisableWebdavAccountsForDeletedRoots
func TestDisableWebdavAccountsForDeletedRoots(t *testing.T) {
	userID, _, abcID := newTestWebdavUser(t)
	ctx := webdavTestCtx(userID)

	username := "dav-" + uuid.New().String()[:8]
	resp, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(abcID),
		Username:   utils.StringPtr(username),
		Password:   utils.StringPtr("correct-horse-battery"),
	})
	if mErr != nil {
		t.Fatalf("create webdav account failed: %v", mErr)
	}
	accountID := resp.GetAccount().GetId()

	DisableWebdavAccountsForDeletedRoots(ctx, []string{abcID})

	stored, err := db.GetWebdavAccountByID(accountID)
	if err != nil {
		t.Fatalf("load account failed: %v", err)
	}
	if stored.Enabled == nil || *stored.Enabled {
		t.Fatalf("expect account disabled by event hook")
	}
	if _, err := AuthenticateWebdav(ctx, username, "correct-horse-battery"); err == nil {
		t.Errorf("expect auth failure for auto disabled account")
	}

	// 不相关目录的删除不应影响账号
	DisableWebdavAccountsForDeletedRoots(ctx, []string{uuid.New().String()})
	stored, err = db.GetWebdavAccountByID(accountID)
	if err != nil {
		t.Fatalf("load account failed: %v", err)
	}
	if stored.Enabled != nil && *stored.Enabled {
		t.Errorf("unrelated deletion must not re-enable a disabled account")
	}
}

// ===== 重置密码 / 删除 / 越权 =====

func TestResetPasswordDeleteAndOwnership(t *testing.T) {
	userID, _, abcID := newTestWebdavUser(t)
	ctx := webdavTestCtx(userID)

	username := "dav-" + uuid.New().String()[:8]
	oldPassword := "old-password-1234"
	resp, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(abcID),
		Username:   utils.StringPtr(username),
		Password:   utils.StringPtr(oldPassword),
	})
	if mErr != nil {
		t.Fatalf("create webdav account failed: %v", mErr)
	}
	accountID := resp.GetAccount().GetId()

	// 重置密码（留空 → 服务端生成）
	newPwdResp, mErr := ResetWebdavAccountPassword(ctx, &native.WebdavAccountResetPasswordReq{
		Id: utils.Uint32Ptr(accountID),
	})
	if mErr != nil {
		t.Fatalf("reset password failed: %v", mErr)
	}
	newPassword := newPwdResp.GetPassword()
	if newPassword == "" || newPassword == oldPassword {
		t.Fatalf("expect a new generated password, got %q", newPassword)
	}
	if _, err := AuthenticateWebdav(ctx, username, oldPassword); err == nil {
		t.Errorf("expect old password to be invalid after reset")
	}
	if _, err := AuthenticateWebdav(ctx, username, newPassword); err != nil {
		t.Fatalf("expect new password to work, got %v", err)
	}

	// 他人不能改/删我的账号
	otherUserID, _, _ := newTestWebdavUser(t)
	otherCtx := webdavTestCtx(otherUserID)
	if mErr := UpdateWebdavAccount(otherCtx, &native.WebdavAccountUpdateReq{
		Id: utils.Uint32Ptr(accountID),
	}); mErr == nil || mErr.C != native.RespCode_NOT_FOUND {
		t.Fatalf("expect NOT_FOUND when updating other user's account, got %v", mErr)
	}
	if mErr := DeleteWebdavAccount(otherCtx, &native.WebdavAccountDeleteReq{
		Id: utils.Uint32Ptr(accountID),
	}); mErr == nil || mErr.C != native.RespCode_NOT_FOUND {
		t.Fatalf("expect NOT_FOUND when deleting other user's account, got %v", mErr)
	}

	// 更新只读/启用（注意 false 值必须真正写库）
	if mErr := UpdateWebdavAccount(ctx, &native.WebdavAccountUpdateReq{
		Id:       utils.Uint32Ptr(accountID),
		ReadOnly: utils.BoolPtr(true),
	}); mErr != nil {
		t.Fatalf("update read_only failed: %v", mErr)
	}
	stored, err := db.GetWebdavAccountByID(accountID)
	if err != nil {
		t.Fatalf("load account failed: %v", err)
	}
	if stored.ReadOnly == nil || !*stored.ReadOnly {
		t.Errorf("expect read_only=true persisted")
	}
	if mErr := UpdateWebdavAccount(ctx, &native.WebdavAccountUpdateReq{
		Id:       utils.Uint32Ptr(accountID),
		ReadOnly: utils.BoolPtr(false),
		Enabled:  utils.BoolPtr(false),
	}); mErr != nil {
		t.Fatalf("update to false failed: %v", mErr)
	}
	stored, err = db.GetWebdavAccountByID(accountID)
	if err != nil {
		t.Fatalf("load account failed: %v", err)
	}
	if stored.ReadOnly == nil || *stored.ReadOnly {
		t.Errorf("expect read_only=false persisted (GORM 零值陷阱)")
	}
	if stored.Enabled == nil || *stored.Enabled {
		t.Errorf("expect enabled=false persisted")
	}

	// 删除后认证失败、列表为空
	if mErr := DeleteWebdavAccount(ctx, &native.WebdavAccountDeleteReq{
		Id: utils.Uint32Ptr(accountID),
	}); mErr != nil {
		t.Fatalf("delete failed: %v", mErr)
	}
	if _, err := db.GetWebdavAccountByID(accountID); err == nil {
		t.Errorf("expect account deleted")
	}
	list, mErr := ListWebdavAccounts(ctx)
	if mErr != nil {
		t.Fatalf("list failed: %v", mErr)
	}
	if len(list.GetAccounts()) != 0 {
		t.Errorf("expect empty list after delete, got %d", len(list.GetAccounts()))
	}
	if _, err := AuthenticateWebdav(ctx, username, newPassword); err == nil {
		t.Errorf("expect auth failure after account deleted")
	}
}

// 路径渲染：绑定用户根目录时 root_path 应为 "/"
func TestListWebdavAccountsRootPath(t *testing.T) {
	userID, rootID, _ := newTestWebdavUser(t)
	ctx := webdavTestCtx(userID)

	username := "dav-" + uuid.New().String()[:8]
	if _, mErr := CreateWebdavAccount(ctx, &native.WebdavAccountCreateReq{
		RootFileId: utils.StringPtr(rootID),
		Username:   utils.StringPtr(username),
		Password:   utils.StringPtr("correct-horse-battery"),
	}); mErr != nil {
		t.Fatalf("create webdav account failed: %v", mErr)
	}
	list, mErr := ListWebdavAccounts(ctx)
	if mErr != nil {
		t.Fatalf("list failed: %v", mErr)
	}
	if len(list.GetAccounts()) != 1 {
		t.Fatalf("expect 1 account, got %d", len(list.GetAccounts()))
	}
	if got := list.GetAccounts()[0].GetRootPath(); got != "/" {
		t.Errorf("expect root_path /, got %q", got)
	}
}

// 用户名唯一索引兜底：绕过 service 预检直接插同名也应失败（并发场景）
func TestWebdavUsernameUniqueIndex(t *testing.T) {
	_, _, abcID := newTestWebdavUser(t)
	username := "uniq-" + uuid.New().String()[:8]
	userID := uuid.New().String()

	first := &entity.WebdavAccountORM{
		UserId:       utils.StringPtr(userID),
		Username:     utils.StringPtr(username),
		PasswordHash: utils.StringPtr("x"),
		RootFileId:   utils.StringPtr(abcID),
		Enabled:      utils.BoolPtr(true),
	}
	if err := db.CreateWebdavAccount(first); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	second := &entity.WebdavAccountORM{
		UserId:       utils.StringPtr(uuid.New().String()),
		Username:     utils.StringPtr(username),
		PasswordHash: utils.StringPtr("y"),
		RootFileId:   utils.StringPtr(abcID),
		Enabled:      utils.BoolPtr(true),
	}
	err := db.CreateWebdavAccount(second)
	if err == nil {
		t.Fatalf("expect unique index violation for duplicate username")
	}
	if !isWebdavUsernameTaken(err) {
		t.Errorf("expect duplicate error to be recognized as username conflict, got %v", err)
	}
	if !errors.Is(err, gorm.ErrDuplicatedKey) && !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Errorf("unexpected duplicate error shape: %v", err)
	}
}
