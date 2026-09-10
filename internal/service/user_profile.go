package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // 注册 PNG 解码器
	"os"
	"path/filepath"
	"strings"
	"time"

	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/llog"
	"jenken827.com/jenken827/pfm/internal/models"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"golang.org/x/image/draw"
)

// 头像规格常量
const (
	avatarSize       = 256                 // 输出正方形边长（像素）
	avatarMaxRawSize = 8 << 20             // 上传原图上限 8MB
	avatarDirName    = "avatars"           // 存放目录（位于 DataDir 下，不进网盘、不计配额）
	avatarExt        = ".jpg"              // 统一输出 JPEG（体积小，兼容性好）
	avatarURLPrefix  = "/api/user/avatar/" // 对外相对路径前缀
)

// avatarDir 返回头像目录绝对路径（不保证存在）
func avatarDir() string {
	return filepath.Join(globals.DataDir, avatarDirName)
}

// AvatarFilePath 返回指定用户的头像文件路径
func AvatarFilePath(userID string) string {
	return filepath.Join(avatarDir(), userID+avatarExt)
}

// UpdateProfile 更新当前登录用户的资料（用户名 / 邮箱，仅本人）
func UpdateProfile(ctx context.Context, req *native.UpdateProfileReq) (*native.UpdateProfileResp, *models.Err) {
	userID := getCurrentUserIDFromCtx(ctx)
	if userID == "" {
		return nil, &models.Err{E: errors.New("unauthorized"), C: native.RespCode_PERMISSION_DENIED}
	}

	username := strings.TrimSpace(req.GetUsername())
	if username == "" {
		return nil, &models.Err{E: errors.New("username is required"), C: native.RespCode_PARAMS_ERROR}
	}
	if len(username) > 64 {
		return nil, &models.Err{E: errors.New("username too long (max 64)"), C: native.RespCode_PARAMS_ERROR}
	}
	email := strings.TrimSpace(req.GetEmail())
	if len(email) > 128 {
		return nil, &models.Err{E: errors.New("email too long (max 128)"), C: native.RespCode_PARAMS_ERROR}
	}

	// 用户名唯一性（排除自己）
	taken, err := db.IsUsernameTaken(username, userID)
	if err != nil {
		llog.C(ctx).Errorf("check username taken failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	if taken {
		return nil, &models.Err{E: errors.New("username already taken"), C: native.RespCode_PARAMS_ERROR}
	}

	if err := db.UpdateUserProfile(userID, username, email); err != nil {
		llog.C(ctx).Errorf("update profile failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	user, err := db.GetUserByID(userID)
	if err != nil {
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}
	llog.C(ctx).Infof("profile updated: user_id=%s, username=%s", userID, username)
	return &native.UpdateProfileResp{
		Username: user.Username,
		Email:    user.Email,
		Avatar:   user.Avatar,
	}, nil
}

// SaveAvatar 处理并保存用户头像。
//
// 流程：校验格式/尺寸 → 中心裁剪为 256×256 正方形 → 统一编码 JPEG → 覆盖写入
// <DataDir>/avatars/<userID>.jpg → 更新 users.avatar（带 ?v= 时间戳破缓存）。
//
// raw 为上传的原始图片字节；ext 用于格式判定（如 ".png" / ".jpg"）。
func SaveAvatar(ctx context.Context, userID string, raw []byte) (*native.UploadAvatarResp, *models.Err) {
	if userID == "" {
		return nil, &models.Err{E: errors.New("unauthorized"), C: native.RespCode_PERMISSION_DENIED}
	}
	if len(raw) == 0 {
		return nil, &models.Err{E: errors.New("empty upload"), C: native.RespCode_PARAMS_ERROR}
	}
	if len(raw) > avatarMaxRawSize {
		return nil, &models.Err{
			E: fmt.Errorf("image too large (max %dMB)", avatarMaxRawSize>>20),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, &models.Err{
			E: errors.New("unsupported image format (expect jpeg/png/gif)"),
			C: native.RespCode_PARAMS_ERROR,
		}
	}

	square := centerCropSquare(src, avatarSize)

	if err := os.MkdirAll(avatarDir(), 0o755); err != nil {
		llog.C(ctx).Errorf("create avatar dir failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	target := AvatarFilePath(userID)
	// 先写临时文件再重命名，避免并发读写到半张图
	tmp := target + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		llog.C(ctx).Errorf("create avatar temp file failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	if err := jpeg.Encode(f, square, &jpeg.Options{Quality: 88}); err != nil {
		f.Close()
		os.Remove(tmp)
		llog.C(ctx).Errorf("encode avatar failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}
	f.Close()
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		llog.C(ctx).Errorf("rename avatar failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_INTERNAL_ERROR}
	}

	// 带时间戳破缓存
	url := fmt.Sprintf("%s%s?v=%d", avatarURLPrefix, userID, time.Now().Unix())
	if err := db.UpdateUserAvatar(userID, url); err != nil {
		llog.C(ctx).Errorf("update user avatar failed: %v", err)
		return nil, &models.Err{E: err, C: native.RespCode_DB_ERROR}
	}

	llog.C(ctx).Infof("avatar saved: user_id=%s", userID)
	return &native.UploadAvatarResp{Avatar: &url}, nil
}

// centerCropSquare 将图片中心裁剪为边长 size 的正方形并缩放。
// 取短边作为裁剪边长，保证不变形（等比填满后居中裁切）。
func centerCropSquare(src image.Image, size int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	side := w
	if h < side {
		side = h
	}
	// 居中裁剪矩形
	ox := b.Min.X + (w-side)/2
	oy := b.Min.Y + (h-side)/2
	crop := image.Rect(ox, oy, ox+side, oy+side)

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)
	return dst
}
