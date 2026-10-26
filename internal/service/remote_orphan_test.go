package service

import (
	"context"
	"errors"
	"path"
	"testing"

	"jenken827.com/jenken827/pfm/driver/types"
)

// fakeRemoteLister 用内存目录树模拟驱动的远端枚举能力
type fakeRemoteLister struct {
	tree map[string][]types.RemoteEntry
	errs map[string]error
	// loopDir 为 true 时，任意目录下都返回一个同名子目录，用来构造目录环
	loopDir bool
}

func (f *fakeRemoteLister) RemoteRoot() string { return "/" }

func (f *fakeRemoteLister) ListRemote(_ context.Context, dir string) ([]types.RemoteEntry, error) {
	if err, ok := f.errs[dir]; ok {
		return nil, err
	}
	if f.loopDir {
		return []types.RemoteEntry{{Path: path.Join(dir, "loop"), Name: "loop", IsDir: true}}, nil
	}
	return f.tree[dir], nil
}

func knownSet(paths ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		out[canonicalRemotePath(p)] = struct{}{}
	}
	return out
}

// TestWalkRemoteTreeCounts 只有文件参与统计：目录不下钻计数，有记录的不算孤儿
func TestWalkRemoteTreeCounts(t *testing.T) {
	lister := &fakeRemoteLister{tree: map[string][]types.RemoteEntry{
		"/2026": {
			{Path: "/2026/01", Name: "01", IsDir: true},
			{Path: "/2026/top.jpg", Name: "top.jpg", ID: "1", Size: 10},
		},
		"/2026/01": {
			{Path: "/2026/01/known.jpg", Name: "known.jpg", ID: "2", Size: 20},
			{Path: "/2026/01/orphan.jpg", Name: "orphan.jpg", ID: "3", Size: 30},
		},
	}}
	result := &RemoteOrphanScan{Root: "/2026"}
	if err := walkRemoteTree(context.Background(), lister, "/2026", 0,
		knownSet("/2026/01/known.jpg", "/2026/01"), result, nil); err != nil {
		t.Fatalf("walk 失败: %v", err)
	}
	if result.Scanned != 3 {
		t.Fatalf("应扫描 3 个文件, got %d", result.Scanned)
	}
	if result.Recorded != 1 {
		t.Fatalf("应有 1 个文件带数据库记录, got %d", result.Recorded)
	}
	if len(result.Orphans) != 2 {
		t.Fatalf("应有 2 个孤儿, got %d (%+v)", len(result.Orphans), result.Orphans)
	}
	// 目录本身即使不在库里也不能当孤儿
	for _, o := range result.Orphans {
		if o.Path == "/2026/01" {
			t.Fatalf("目录不应被当成孤儿: %+v", o)
		}
	}
	if result.Orphans[0].Path != "/2026/01/orphan.jpg" || result.Orphans[1].Path != "/2026/top.jpg" {
		t.Fatalf("孤儿路径不符: %+v", result.Orphans)
	}
	if result.Orphans[0].ID != "3" || result.Orphans[1].Size != 10 {
		t.Fatalf("孤儿 ID/大小应透传驱动返回值: %+v", result.Orphans)
	}
}

// TestWalkRemoteTreePathNormalization 比对忽略前导 "/"（兼容老数据路径形态差异）
func TestWalkRemoteTreePathNormalization(t *testing.T) {
	lister := &fakeRemoteLister{tree: map[string][]types.RemoteEntry{
		"/2026": {{Path: "/2026/a.jpg", Name: "a.jpg", ID: "1"}},
	}}
	result := &RemoteOrphanScan{Root: "/2026"}
	// 数据库里存的是不带前导斜杠的历史路径
	if err := walkRemoteTree(context.Background(), lister, "/2026", 0,
		knownSet("2026/a.jpg"), result, nil); err != nil {
		t.Fatalf("walk 失败: %v", err)
	}
	if len(result.Orphans) != 0 || result.Recorded != 1 {
		t.Fatalf("前导斜杠差异不应判成孤儿: recorded=%d orphans=%+v", result.Recorded, result.Orphans)
	}
}

// TestWalkRemoteTreeDepthGuard 目录环要在有限深度内报错，不能无限递归
func TestWalkRemoteTreeDepthGuard(t *testing.T) {
	// 任意目录下都返回一个同名子目录，形成 /loop/loop/... 的死循环
	lister := &fakeRemoteLister{loopDir: true}
	result := &RemoteOrphanScan{Root: "/loop"}
	err := walkRemoteTree(context.Background(), lister, "/loop", 0, knownSet(), result, nil)
	if err == nil {
		t.Fatalf("目录环应触发深度上限错误")
	}
}

// TestWalkRemoteTreeListError 列目录失败要带路径上抛，不能静默跳过
func TestWalkRemoteTreeListError(t *testing.T) {
	boom := errors.New("boom")
	lister := &fakeRemoteLister{
		tree: map[string][]types.RemoteEntry{},
		errs: map[string]error{"/2026": boom},
	}
	err := walkRemoteTree(context.Background(), lister, "/2026", 0, knownSet(), &RemoteOrphanScan{}, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("应透传驱动错误, got %v", err)
	}
}

func TestCanonicalRemotePath(t *testing.T) {
	cases := map[string]string{
		"/2026/01/a.jpg": "2026/01/a.jpg",
		"2026/01/a.jpg":  "2026/01/a.jpg",
		" /2026/a.jpg ":  "2026/a.jpg",
		"":               "",
	}
	for in, want := range cases {
		if got := canonicalRemotePath(in); got != want {
			t.Fatalf("canonicalRemotePath(%q) = %q, want %q", in, got, want)
		}
	}
}
