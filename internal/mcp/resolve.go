package mcp

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/utils"
	"gorm.io/gorm"
)

// maxPathDepth 路径最大段数，同时也是祖先上溯的深度上限
const maxPathDepth = 64

// maxCandidates 路径找不到时最多列几个同层候选（够模型自纠即可）
const maxCandidates = 10

// resolver 把「令牌视角的路径」解析成 files 记录。
//
// 令牌的可见根目录非空时，路径的 "/" 就是那个目录：解析从根开始逐段向下，
// 天然出不了这棵子树。file_id 入参走 inScope 上溯校验，口径与路径一致。
// 不做任何缓存：协议无会话，缓存会与删除 / 改名脱节。
type resolver struct {
	ownerID string
	rootID  string
}

// newResolver 解析令牌可见的根目录（空 = 用户根目录）并校验归属
func newResolver(principal *auth.McpPrincipal) (*resolver, error) {
	if principal == nil || principal.UserID == "" {
		return nil, fmt.Errorf("缺少调用者身份")
	}
	rootID := principal.RootFolderID
	if rootID == "" {
		space, err := db.GetUserSpaceByUserID(principal.UserID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("该用户还没有根目录，请联系管理员分配")
			}
			return nil, fmt.Errorf("查询用户空间失败: %w", err)
		}
		rootID = utils.DerefString(space.RootFileId)
	}
	if rootID == "" {
		return nil, fmt.Errorf("该用户还没有根目录，请联系管理员分配")
	}
	root, err := db.GetFileByID(rootID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("令牌绑定的根目录已不存在，请重新签发令牌")
		}
		return nil, fmt.Errorf("查询根目录失败: %w", err)
	}
	if fieldOwnerID(root) != principal.UserID {
		return nil, fmt.Errorf("令牌绑定的根目录不属于该用户")
	}
	return &resolver{ownerID: principal.UserID, rootID: rootID}, nil
}

// Root 返回可见根目录记录
func (r *resolver) Root() (*entity.FileORM, error) {
	return r.node("/")
}

// node 按路径取文件或目录；"" 与 "/" 返回根目录
func (r *resolver) node(path string) (*entity.FileORM, error) {
	segments, err := splitPath(path)
	if err != nil {
		return nil, err
	}
	current, err := r.load(r.rootID)
	if err != nil {
		return nil, err
	}
	for index, segment := range segments {
		child, findErr := db.FindFileByParentAndName(fieldID(current), segment, r.ownerID)
		if findErr != nil {
			return nil, fmt.Errorf("查询 %q 失败: %w", segment, findErr)
		}
		if child == nil {
			return nil, r.notFound(segments, index, fieldID(current))
		}
		current = child
	}
	return current, nil
}

// parent 按路径取父目录与末级名（末级允许还不存在，用于建目录 / 写入 / 上传）
func (r *resolver) parent(path string) (*entity.FileORM, string, error) {
	segments, err := splitPath(path)
	if err != nil {
		return nil, "", err
	}
	if len(segments) == 0 {
		return nil, "", fmt.Errorf("路径不能是根目录")
	}
	dirPath := "/" + strings.Join(segments[:len(segments)-1], "/")
	dir, err := r.node(dirPath)
	if err != nil {
		return nil, "", err
	}
	if dirType(dir) != "folder" {
		return nil, "", fmt.Errorf("%s 不是目录", dirPath)
	}
	return dir, segments[len(segments)-1], nil
}

// inScope 判断文件是否在可见根目录内（file_id 入参用：按 parent_id 上溯）
func (r *resolver) inScope(file *entity.FileORM) bool {
	if file == nil || fieldOwnerID(file) != r.ownerID {
		return false
	}
	current := file
	for depth := 0; depth < maxPathDepth; depth++ {
		if fieldID(current) == r.rootID {
			return true
		}
		parentID := fieldParentID(current)
		if parentID == "" || parentID == "0" {
			return false
		}
		parent, err := db.GetFileByID(parentID)
		if err != nil {
			return false
		}
		current = parent
	}
	return false
}

// load 按 id 取记录（不存在时给可读错误）
func (r *resolver) load(id string) (*entity.FileORM, error) {
	file, err := db.GetFileByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("文件或目录不存在")
		}
		return nil, fmt.Errorf("查询文件失败: %w", err)
	}
	if fieldOwnerID(file) != r.ownerID {
		return nil, fmt.Errorf("文件不存在")
	}
	return file, nil
}

// DisplayPath 拼出文件在令牌视角下的完整路径（以根目录为 "/"）
func (r *resolver) DisplayPath(file *entity.FileORM) string {
	if file == nil {
		return "/"
	}
	if fieldID(file) == r.rootID {
		return "/"
	}
	names := make([]string, 0, 8)
	current := file
	for depth := 0; depth < maxPathDepth; depth++ {
		parentID := fieldParentID(current)
		if parentID == "" || parentID == "0" || fieldID(current) == r.rootID {
			break
		}
		names = append(names, fieldName(current))
		parent, err := db.GetFileByID(parentID)
		if err != nil {
			names = append(names, "?")
			break
		}
		current = parent
	}
	for left, right := 0, len(names)-1; left < right; left, right = left+1, right-1 {
		names[left], names[right] = names[right], names[left]
	}
	return "/" + strings.Join(names, "/")
}

// notFound 生成带同层候选的"找不到"错误（模型据此自纠比人肉排查便宜）
func (r *resolver) notFound(segments []string, index int, parentID string) error {
	prefix := "/" + strings.Join(segments[:index+1], "/")
	siblings, err := db.GetFileNamesInParent(parentID, r.ownerID)
	if err != nil || len(siblings) == 0 {
		return fmt.Errorf("路径不存在: %s", prefix)
	}
	names := make([]string, 0, len(siblings))
	for name := range siblings {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxCandidates {
		names = names[:maxCandidates]
	}
	return fmt.Errorf("路径不存在: %s（同层有: %s）", prefix, strings.Join(names, ", "))
}

// splitPath 把路径切成段；"" 与 "/" 返回空切片
func splitPath(path string) ([]string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "/" {
		return nil, nil
	}
	if !strings.HasPrefix(trimmed, "/") {
		return nil, fmt.Errorf("路径必须以 / 开头: %q", path)
	}
	raw := strings.Split(strings.Trim(trimmed, "/"), "/")
	segments := make([]string, 0, len(raw))
	for _, segment := range raw {
		segment = strings.TrimSpace(segment)
		if segment == "" || segment == "." {
			continue
		}
		if segment == ".." {
			return nil, fmt.Errorf("路径不允许包含上级引用（..）")
		}
		segments = append(segments, segment)
	}
	if len(segments) > maxPathDepth {
		return nil, fmt.Errorf("路径层级超过 %d 层", maxPathDepth)
	}
	return segments, nil
}
