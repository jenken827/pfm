package service

import (
	"testing"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

// 复用旧上传记录的字段刷新：version 必须换成"本次请求"发号得到的值。
//
// 历史 bug：复用 initUpload 留下的旧记录时没有刷新 version，分发完成后
// markFileReadyIfVersionMatches 用旧 version 做 CAS，静默命中 0 行，
// 于是位置行全部 PCOMPLETED 的文件永远显示"未就绪"。
func TestUploadRecordReuseUpdatesCarriesCurrentVersion(t *testing.T) {
	providers := `[1,2,4]`

	updates := uploadRecordReuseUpdates("/tmp/x", "IMG.jpg", "parent-1", 1024, "overwrite-1", 7, &providers)

	if got, ok := updates["version"]; !ok || got != uint64(7) {
		t.Fatalf("version 必须刷新成本次发号的值, got %v (ok=%v)", got, ok)
	}
	if got := updates["file_size"]; got != uint64(1024) {
		t.Errorf("file_size 应为本次请求的大小, got %v", got)
	}
	if got := updates["file_name"]; got != "IMG.jpg" {
		t.Errorf("file_name 应为本次请求的文件名, got %v", got)
	}
	if got := updates["parent_id"]; got != "parent-1" {
		t.Errorf("parent_id 应为本次请求的目录, got %v", got)
	}
	if got := updates["task_status"]; got != int32(native.UploadStatus_PENDING) {
		t.Errorf("task_status 应重置为 PENDING, got %v", got)
	}

	// 没有版本上下文（老数据 / 非上传入口）时不能写出 version=0，
	// 否则会把记录上的版本号抹掉，CAS 变成"只认版本 0"。
	bare := uploadRecordReuseUpdates("/tmp/x", "IMG.jpg", "parent-1", 1024, "", 0, &providers)
	if _, ok := bare["version"]; ok {
		t.Error("version=0 时不应写 version 字段")
	}
}
