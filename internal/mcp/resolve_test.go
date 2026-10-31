package mcp

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/dbtest"
	"github.com/jenken827/pfm/utils"
)

// 这些用例验证"路径 ↔ id"与令牌可见根目录这两条不变式。
// dbtest 在整个测试包内共享一个库，所以数据都带测试名前缀、id 用 uuid。

func newTestFolder(t *testing.T, name, parentID, owner string) string {
	t.Helper()
	return newTestEntry(t, name, parentID, owner, native.FileType_FOLDER)
}

func newTestEntry(t *testing.T, name, parentID, owner string, fileType native.FileType) string {
	t.Helper()
	id := uuid.NewString()
	file := &entity.FileORM{
		Id:               utils.StringPtr(id),
		Name:             utils.StringPtr(name),
		ParentId:         utils.StringPtr(parentID),
		OwnerId:          utils.StringPtr(owner),
		Type:             utils.Int32Ptr(int32(fileType)),
		Hash:             utils.StringPtr(""),
		Mime:             utils.StringPtr("text/plain"),
		Size:             utils.Uint64Ptr(0),
		IsReady:          utils.BoolPtr(true),
		BoundProviderIds: utils.StringPtr("[]"),
	}
	if err := db.CreateFile(file); err != nil {
		t.Fatalf("创建测试文件 %s 失败: %v", name, err)
	}
	return id
}

func newTestUserSpace(t *testing.T, owner, rootID string) {
	t.Helper()
	if err := db.CreateUserSpace(&entity.UserSpaceORM{
		UserId:           utils.StringPtr(owner),
		RootFileId:       utils.StringPtr(rootID),
		BoundProviderIds: utils.StringPtr("[]"),
	}); err != nil {
		t.Fatalf("创建 UserSpace 失败: %v", err)
	}
}

func TestSplitPath(t *testing.T) {
	cases := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"/", 0, false},
		{"/a/b/", 2, false},
		{"/a//b/./c", 3, false},
		{"a/b", 0, true},
		{"/a/../b", 0, true},
	}
	for _, testCase := range cases {
		segments, err := splitPath(testCase.input)
		if testCase.wantErr {
			if err == nil {
				t.Fatalf("%q 应报错", testCase.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q 解析失败: %v", testCase.input, err)
		}
		if len(segments) != testCase.want {
			t.Fatalf("%q 应切成 %d 段，实际 %d（%v）", testCase.input, testCase.want, len(segments), segments)
		}
	}
}

func TestResolvePathAndScope(t *testing.T) {
	dbtest.Setup(t)

	const owner = "u-mcp-resolve"
	rootID := newTestFolder(t, "mcp-root-resolve", "", owner)
	newTestUserSpace(t, owner, rootID)
	docsID := newTestFolder(t, "docs", rootID, owner)
	subID := newTestFolder(t, "sub", docsID, owner)
	fileAID := newTestEntry(t, "a.txt", docsID, owner, native.FileType_DOCUMENT)

	principal := &auth.McpPrincipal{UserID: owner, AllowRead: true}
	res, err := newResolver(principal)
	if err != nil {
		t.Fatalf("构造 resolver 失败: %v", err)
	}

	if node, err := res.node("/"); err != nil || fieldID(node) != rootID {
		t.Fatalf("根路径应解析到 %s，实际 %v err=%v", rootID, node, err)
	}
	if node, err := res.node("/docs/a.txt"); err != nil || fieldID(node) != fileAID {
		t.Fatalf("/docs/a.txt 应解析到 %s，实际 err=%v", fileAID, err)
	}
	if node, err := res.node("/docs/sub"); err != nil || fieldID(node) != subID {
		t.Fatalf("/docs/sub 应解析到 %s，实际 err=%v", subID, err)
	}

	// 找不到时给同层候选，模型据此自纠
	if _, err := res.node("/docs/missing.txt"); err == nil ||
		!strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("路径不存在时应列出同层候选，实际 err=%v", err)
	}

	// 令牌限定在 docs 下："/" 就是 docs，且出不去的
	scoped := &auth.McpPrincipal{UserID: owner, AllowRead: true, RootFolderID: docsID}
	scopedRes, err := newResolver(scoped)
	if err != nil {
		t.Fatalf("限定根目录的 resolver 构造失败: %v", err)
	}
	if node, err := scopedRes.node("/"); err != nil || fieldID(node) != docsID {
		t.Fatalf("限定根目录时 / 应解析到 %s，实际 err=%v", docsID, err)
	}
	if node, err := scopedRes.node("/sub"); err != nil || fieldID(node) != subID {
		t.Fatalf("限定根目录时 /sub 应解析到 %s，实际 err=%v", subID, err)
	}
	if _, err := scopedRes.node("/docs"); err == nil {
		t.Fatalf("限定根目录后应看不到根目录本身的名字")
	}

	// file_id 入参走 inScope 上溯校验
	fileA, err := res.load(fileAID)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if !scopedRes.inScope(fileA) {
		t.Fatalf("docs 下的文件应在 docs 令牌的可见范围内")
	}
	rootFile, err := res.load(rootID)
	if err != nil {
		t.Fatalf("读取根目录失败: %v", err)
	}
	if scopedRes.inScope(rootFile) {
		t.Fatalf("根目录本身不应在 docs 令牌的可见范围内")
	}
}

func TestResolverRejectsForeignRoot(t *testing.T) {
	dbtest.Setup(t)

	const owner = "u-mcp-owner"
	const other = "u-mcp-other"
	rootID := newTestFolder(t, "mcp-root-foreign", "", owner)
	otherFolderID := newTestFolder(t, "foreign", rootID, other)

	// 令牌把"别人的目录"当根：必须在构造期拒绝，而不是等到解析时
	principal := &auth.McpPrincipal{UserID: owner, RootFolderID: otherFolderID}
	if _, err := newResolver(principal); err == nil {
		t.Fatalf("根目录不属于该用户时应报错")
	}
}

func TestResolverRequiresUserSpace(t *testing.T) {
	dbtest.Setup(t)

	principal := &auth.McpPrincipal{UserID: "u-mcp-no-space"}
	if _, err := newResolver(principal); err == nil {
		t.Fatalf("没有 UserSpace 时应给可读错误")
	}
}
