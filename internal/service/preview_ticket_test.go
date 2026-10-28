package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jenken827/pfm-proto/proto/gen/entity"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/auth"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/internal/dbtest"
	"github.com/jenken827/pfm/utils"
)

// createTestFile 建一个属于 ownerID 的普通文件（票据只关心归属，不需要真实内容）
func createTestFile(t *testing.T, ownerID, parentID, name string) string {
	t.Helper()
	id := uuid.New().String()
	file := &entity.FileORM{
		Id:       utils.StringPtr(id),
		Name:     utils.StringPtr(name),
		ParentId: utils.StringPtr(parentID),
		OwnerId:  utils.StringPtr(ownerID),
		Type:     utils.EnumPtr(native.FileType_DOCUMENT),
		Size:     utils.Uint64Ptr(1),
		IsReady:  utils.BoolPtr(true),
	}
	if err := db.CreateFile(file); err != nil {
		t.Fatalf("create test file %s failed: %v", name, err)
	}
	return id
}

// TestCreatePreviewTicketBindsOwnerAndFile 票据必须绑定"这一个文件 + 这一个用户"，
// 有效期是服务端固定的 10 分钟（客户端无法指定更长的时效）
func TestCreatePreviewTicketBindsOwnerAndFile(t *testing.T) {
	dbtest.Setup(t)
	ownerID := uuid.New().String()
	rootID := createTestFolder(t, ownerID, "root-"+ownerID, "")
	fileID := createTestFile(t, ownerID, rootID, "report.pdf")

	resp, mErr := CreatePreviewTicket(webdavTestCtx(ownerID), &native.CreatePreviewTicketReq{
		FileId: utils.StringPtr(fileID),
	})
	if mErr != nil {
		t.Fatalf("create preview ticket failed: %v", mErr)
	}
	if resp.GetTicket() == "" {
		t.Fatal("empty ticket")
	}

	claims, err := auth.GetLocalAuth().VerifyPreviewTicket(resp.GetTicket())
	if err != nil {
		t.Fatalf("verify ticket failed: %v", err)
	}
	if claims.FileID != fileID {
		t.Fatalf("ticket file = %q, want %q", claims.FileID, fileID)
	}
	if claims.UserID != ownerID {
		t.Fatalf("ticket user = %q, want %q", claims.UserID, ownerID)
	}
	if ttl := time.Until(claims.ExpireAt); ttl <= 0 || ttl > previewTicketTTL {
		t.Fatalf("unexpected ticket ttl: %v", ttl)
	}
}

// TestCreatePreviewTicketRejectsNonOwner 不能给别人（或别人的文件）签票据
func TestCreatePreviewTicketRejectsNonOwner(t *testing.T) {
	dbtest.Setup(t)
	ownerID := uuid.New().String()
	otherID := uuid.New().String()
	rootID := createTestFolder(t, ownerID, "root-"+ownerID, "")
	fileID := createTestFile(t, ownerID, rootID, "private.pdf")

	_, mErr := CreatePreviewTicket(webdavTestCtx(otherID), &native.CreatePreviewTicketReq{
		FileId: utils.StringPtr(fileID),
	})
	if mErr == nil {
		t.Fatal("want permission error for non-owner")
	}
	if mErr.C != native.RespCode_PERMISSION_DENIED {
		t.Fatalf("code = %v, want PERMISSION_DENIED", mErr.C)
	}
}

// TestCreatePreviewTicketRejectsMissingFileID 缺 file_id 必须是参数错误
func TestCreatePreviewTicketRejectsMissingFileID(t *testing.T) {
	dbtest.Setup(t)

	_, mErr := CreatePreviewTicket(webdavTestCtx(uuid.New().String()), &native.CreatePreviewTicketReq{})
	if mErr == nil {
		t.Fatal("want params error for empty file_id")
	}
	if mErr.C != native.RespCode_PARAMS_ERROR {
		t.Fatalf("code = %v, want PARAMS_ERROR", mErr.C)
	}
}

// TestCreatePreviewTicketDownloadPurpose 下载票的有效期是 12 小时（预览票的 10 分钟只够一次性拉取）
func TestCreatePreviewTicketDownloadPurpose(t *testing.T) {
	dbtest.Setup(t)
	ownerID := uuid.New().String()
	rootID := createTestFolder(t, ownerID, "root-"+ownerID, "")
	fileID := createTestFile(t, ownerID, rootID, "movie.mp4")

	resp, mErr := CreatePreviewTicket(webdavTestCtx(ownerID), &native.CreatePreviewTicketReq{
		FileId:  utils.StringPtr(fileID),
		Purpose: native.TicketPurpose_DOWNLOAD.Enum(),
	})
	if mErr != nil {
		t.Fatalf("create download ticket failed: %v", mErr)
	}

	claims, err := auth.GetLocalAuth().VerifyPreviewTicket(resp.GetTicket())
	if err != nil {
		t.Fatalf("verify ticket failed: %v", err)
	}
	if claims.Purpose != auth.PurposeDownload {
		t.Fatalf("ticket purpose = %q, want %q", claims.Purpose, auth.PurposeDownload)
	}
	ttl := time.Until(claims.ExpireAt)
	if ttl <= previewTicketTTL || ttl > downloadTicketTTL {
		t.Fatalf("download ticket ttl = %v, want in (%v, %v]", ttl, previewTicketTTL, downloadTicketTTL)
	}
}

// TestTicketTTLForPurpose 用途决定有效期；未知 / 空用途一律按预览（保守）
func TestTicketTTLForPurpose(t *testing.T) {
	cases := []struct {
		purpose string
		want    time.Duration
	}{
		{auth.PurposePreview, previewTicketTTL},
		{auth.PurposeDownload, downloadTicketTTL},
		{"", previewTicketTTL},
		{"unknown-purpose", previewTicketTTL},
	}
	for _, c := range cases {
		if got := ticketTTLForPurpose(c.purpose); got != c.want {
			t.Fatalf("ticketTTLForPurpose(%q) = %v, want %v", c.purpose, got, c.want)
		}
	}
}

// TestSignDownloadTicket 直链 302 回退时服务端内部签的票必须是下载票（12 小时）
func TestSignDownloadTicket(t *testing.T) {
	dbtest.Setup(t)
	fileID := uuid.New().String()
	userID := uuid.New().String()

	ticket, err := SignDownloadTicket(fileID, userID)
	if err != nil {
		t.Fatalf("sign download ticket failed: %v", err)
	}
	claims, err := auth.GetLocalAuth().VerifyPreviewTicket(ticket)
	if err != nil {
		t.Fatalf("verify ticket failed: %v", err)
	}
	if claims.FileID != fileID || claims.UserID != userID {
		t.Fatalf("ticket binding = (%q, %q), want (%q, %q)", claims.FileID, claims.UserID, fileID, userID)
	}
	if claims.Purpose != auth.PurposeDownload {
		t.Fatalf("purpose = %q, want %q", claims.Purpose, auth.PurposeDownload)
	}
	if ttl := time.Until(claims.ExpireAt); ttl <= previewTicketTTL || ttl > downloadTicketTTL {
		t.Fatalf("ttl = %v, want in (%v, %v]", ttl, previewTicketTTL, downloadTicketTTL)
	}
}
