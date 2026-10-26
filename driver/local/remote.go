package local

import (
	"context"
	"os"
	"path/filepath"

	"jenken827.com/jenken827/pfm/driver/types"
)

var _ types.RemoteLister = (*LocalDriver)(nil)

// RemoteRoot 实现 types.RemoteLister：本地 provider 的 pfm 数据根目录（绝对路径）
func (l *LocalDriver) RemoteRoot() string {
	if l.Config == nil || l.Config.RootPath == nil {
		return ""
	}
	return *l.Config.RootPath
}

// ListRemote 实现 types.RemoteLister：列出本地目录下的直接子项。
// Path 用 filepath.Join 拼成绝对路径，与 Upload 写入 file_locations.path 的形态一致。
func (l *LocalDriver) ListRemote(ctx context.Context, dir string) ([]types.RemoteEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]types.RemoteEntry, 0, len(entries))
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		size := int64(0)
		if info, ierr := e.Info(); ierr == nil {
			size = info.Size()
		}
		out = append(out, types.RemoteEntry{
			Path:  full,
			Name:  e.Name(),
			ID:    full, // 本地删除按路径，ID 也填路径便于统一构造 FileRef
			IsDir: e.IsDir(),
			Size:  size,
		})
	}
	return out, nil
}
