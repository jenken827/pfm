package service

import (
	"testing"
	"time"

	"jenken827.com/jenken827/pfm/proto/gen/native"
)

func namePtr(s string) *string  { return &s }
func idPtr(s string) *string    { return &s }
func timePtr(t time.Time) *time.Time { return &t }

func TestPickKeeper(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id, name string, created time.Time) *native.FileORM {
		return &native.FileORM{
			Id:        idPtr(id),
			Name:      namePtr(name),
			CreatedAt: timePtr(created),
		}
	}

	t.Run("保留名称最短", func(t *testing.T) {
		group := []*native.FileORM{
			mk("a", "mmexport1713701399029.jpg", base),
			mk("b", "IMG_1.jpg", base.Add(time.Hour)),
			mk("c", "Screenshot_20240109_xxx.jpg", base.Add(2*time.Hour)),
		}
		keeper := pickKeeper(group)
		if *keeper.Id != "b" {
			t.Fatalf("want keeper b (shortest name), got %s (%s)", *keeper.Id, *keeper.Name)
		}
	})

	t.Run("同长度保留最早创建", func(t *testing.T) {
		group := []*native.FileORM{
			mk("a", "IMG_11.jpg", base.Add(3*time.Hour)),
			mk("b", "IMG_22.jpg", base), // 同名长度，更早
		}
		keeper := pickKeeper(group)
		if *keeper.Id != "b" {
			t.Fatalf("want keeper b (older), got %s", *keeper.Id)
		}
	})

	t.Run("同长度同时创建保留id较小", func(t *testing.T) {
		group := []*native.FileORM{
			mk("z", "IMG_33.jpg", base),
			mk("a", "IMG_44.jpg", base),
		}
		keeper := pickKeeper(group)
		if *keeper.Id != "a" {
			t.Fatalf("want keeper a (smaller id), got %s", *keeper.Id)
		}
	})

	t.Run("中文名按字符数", func(t *testing.T) {
		group := []*native.FileORM{
			mk("a", "照片备份最终版最终版.jpg", base), // 12 字符
			mk("b", "照片.jpg", base),                 // 5 字符
		}
		keeper := pickKeeper(group)
		if *keeper.Id != "b" {
			t.Fatalf("want keeper b (5 chars), got %s", *keeper.Id)
		}
	})

	t.Run("单元素组返回自身", func(t *testing.T) {
		group := []*native.FileORM{mk("a", "only.jpg", base)}
		if *pickKeeper(group).Id != "a" {
			t.Fatal("single element group should return itself")
		}
	})
}
