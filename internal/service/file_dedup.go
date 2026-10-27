package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"jenken827.com/jenken827/pfm/driver/types"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/internal/db"
	"jenken827.com/jenken827/pfm/internal/storage"
	"jenken827.com/jenken827/pfm/internal/thumbnail"
	"jenken827.com/jenken827/pfm/pkg/llog"
	"jenken827.com/jenken827/pfm/proto/gen/entity"
	"jenken827.com/jenken827/pfm/proto/gen/native"
	"jenken827.com/jenken827/pfm/utils"
)

// DedupResult 去重结果统计
type DedupResult struct {
	Scanned         int      // 参与去重的文件数（有 hash 的非文件夹文件）
	DuplicateGroups int      // 存在重复的 hash 组数
	ToDelete        int      // 待删除的冗余文件数
	Deleted         int      // 实际删除数（dry-run 为 0）
	Kept            int      // 保留的文件数（= DuplicateGroups）
	DryRun          bool     // 是否为预览模式
	Details         []string // 每组明细
}

// dedupBaseOwnerID 校验去重根目录（必须为文件夹且带 owner），返回 ownerID
func dedupBaseOwnerID(baseParentID string) (string, error) {
	base, err := db.GetFileByID(baseParentID)
	if err != nil {
		return "", fmt.Errorf("查询基础目录失败: %w", err)
	}
	if base.Type == nil || *base.Type != int32(native.FileType_FOLDER) {
		return "", fmt.Errorf("parent_id %s 不是文件夹", baseParentID)
	}
	ownerID := strPtrVal(base.OwnerId)
	if ownerID == "" {
		return "", fmt.Errorf("基础目录缺少 owner_id，无法去重")
	}
	return ownerID, nil
}

// collectSubtreeFiles 递归收集子树下所有非文件夹文件（不含软删除）
func collectSubtreeFiles(baseParentID, ownerID string) ([]*entity.FileORM, error) {
	var files []*entity.FileORM
	var walk func(parentID string) error
	walk = func(parentID string) error {
		children, err := db.GetFileChildren(parentID, ownerID)
		if err != nil {
			return err
		}
		for _, c := range children {
			if c.Id == nil || c.Name == nil || c.ParentId == nil {
				continue
			}
			if c.Type != nil && *c.Type == int32(native.FileType_FOLDER) {
				if err := walk(*c.Id); err != nil {
					return err
				}
				continue
			}
			files = append(files, c)
		}
		return nil
	}
	if err := walk(baseParentID); err != nil {
		return nil, fmt.Errorf("扫描目录失败: %w", err)
	}
	return files, nil
}

// DedupFilesByHash 扫描 baseParentID 子树下所有文件，按内容 hash 去重：
// 同一 hash 出现多份时，保留文件名最短的一个（同长度保留最早创建的），
// 其余冗余文件永久删除。
//
// 安全性（物理存储按 hash 共享）：
//   - file_locations 按 blake3_hash 关联，删除副本记录不影响幸存文件的存储位置
//   - 缩略图按 hash 命名，幸存文件继续使用，不清理
//   - 提供者（百度/Telegram 等）上的物理数据按 hash 共享，不删除
//   - 仅删除冗余的 files 记录及其 file_histories 行
//
// dryRun 为 true 时只统计与预览，不删除任何记录。
func DedupFilesByHash(baseParentID string, dryRun bool) (*DedupResult, error) {
	ownerID, err := dedupBaseOwnerID(baseParentID)
	if err != nil {
		return nil, err
	}

	result := &DedupResult{DryRun: dryRun}

	files, err := collectSubtreeFiles(baseParentID, ownerID)
	if err != nil {
		return nil, err
	}

	// 按 hash 分组
	byHash := make(map[string][]*entity.FileORM)
	for _, f := range files {
		if f.Hash == nil || *f.Hash == "" {
			continue
		}
		result.Scanned++
		byHash[*f.Hash] = append(byHash[*f.Hash], f)
	}

	// 排序保证输出稳定（按 hash 字典序）
	hashes := make([]string, 0, len(byHash))
	for h := range byHash {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	var toDelete []string
	for _, hash := range hashes {
		group := byHash[hash]
		if len(group) < 2 {
			continue
		}
		result.DuplicateGroups++

		keeper := pickKeeper(group)
		result.Details = append(result.Details, formatDedupDetail(hash, keeper, group))

		for _, f := range group {
			if f.Id != nil && *f.Id != *keeper.Id {
				toDelete = append(toDelete, *f.Id)
				result.ToDelete++
			}
		}
	}
	result.Kept = result.DuplicateGroups

	if dryRun || len(toDelete) == 0 {
		return result, nil
	}

	// 执行删除：仅删 files 记录 + file_histories 行。
	// 注意：不走 permanentlyDeleteFiles（其会按 hash 删缩略图、且删历史物理文件），
	// 这里冗余文件与幸存文件共享 hash，缩略图/存储位置/提供者数据必须保留。
	if err := db.DeletePermanently(toDelete); err != nil {
		return nil, fmt.Errorf("删除冗余文件失败: %w", err)
	}
	if err := db.DeleteFileHistoryByFileIDs(toDelete); err != nil {
		llog.L().Warnf("dedup: 清理 file_histories 失败: %v", err)
	}
	result.Deleted = len(toDelete)
	llog.L().Infof("dedup: deleted %d duplicate file records (%d hash groups)", len(toDelete), result.DuplicateGroups)
	return result, nil
}

// pickKeeper 从同 hash 文件组中选出保留者：名称最短，同长度保留最早创建的，再同保留 id 较小
func pickKeeper(group []*entity.FileORM) *entity.FileORM {
	keeper := group[0]
	for _, f := range group[1:] {
		if betterKeeper(f, keeper) {
			keeper = f
		}
	}
	return keeper
}

// betterKeeper 判断 candidate 是否比 current 更适合保留
func betterKeeper(candidate, current *entity.FileORM) bool {
	cLen := nameLen(candidate)
	kLen := nameLen(current)
	if cLen != kLen {
		return cLen < kLen
	}
	cTime := fileCreatedAt(candidate)
	kTime := fileCreatedAt(current)
	if !cTime.Equal(kTime) {
		return cTime.Before(kTime)
	}
	return strPtrVal(candidate.Id) < strPtrVal(current.Id)
}

// nameLen 文件名长度（按字符数计算，中文名按可见长度）
func nameLen(f *entity.FileORM) int {
	if f.Name == nil {
		return 0
	}
	return utf8.RuneCountInString(*f.Name)
}

// fileCreatedAt 文件创建时间（nil 按零值处理）
func fileCreatedAt(f *entity.FileORM) time.Time {
	if f.CreatedAt != nil {
		return *f.CreatedAt
	}
	return time.Time{}
}

// formatDedupDetail 格式化单组去重明细，便于人工核对
func formatDedupDetail(hash string, keeper *entity.FileORM, group []*entity.FileORM) string {
	var delNames []string
	for _, f := range group {
		if f.Id != nil && keeper.Id != nil && *f.Id == *keeper.Id {
			continue
		}
		delNames = append(delNames, truncateStr(strPtrVal(f.Name), 30))
	}
	return fmt.Sprintf("hash=%s: 保留 %q (%d字符), 删除 %d 个: %s",
		truncateStr(hash, 10), truncateStr(strPtrVal(keeper.Name), 40),
		nameLen(keeper), len(delNames), strings.Join(delNames, ", "))
}

// truncateStr 截断字符串用于展示
func truncateStr(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

// ============================================================================
// 文件名后缀副本去重（"xxx (1).jpg" vs "xxx.jpg"）—— dedup-files 的阶段 2
// 背景：浏览器/IM 下载同名文件会自动追加 " (1)"，两次迁移/上传的同名照片
// 内容像素一致但 EXIF 元数据不同（blake3 不同），按 hash 去重永远无法命中。
// 规则：同目录同名配对后先比文件大小（大的一方优先保留，不因带后缀就直接删），
// 再做内容核验，核验通过才删除。
// ============================================================================

// NameSuffixDedupResult 文件名后缀去重结果统计
type NameSuffixDedupResult struct {
	Scanned       int      // 扫描到的非文件夹文件数
	Groups        int      // 同名配对组数（原文件 + 1..N 个后缀候选）
	Duplicates    int      // 核验为重复的候选文件数（同尺寸：hash 相同/像素一致/字节一致）
	CompressedDup int      // 压缩衍生副本数（不同尺寸但画面一致，删小保大）
	KeepSuffix    int      // 保留后缀副本（删除原文件）的组数
	KeptLarger    int      // 其中因「文件更大」而保留后缀副本的组数
	DiffSize      int      // 同名但尺寸不同且非衍生（需人工确认）
	DiffContent   int      // 同尺寸但内容不同（需人工确认）
	Skipped       int      // 无法核验（无本地副本/JPEG 结构异常/解码失败）
	Unpaired      int      // 带 (N) 后缀但同目录无同名原文件的文件数
	ToDelete      int      // 待删除冗余文件数（dry-run 预览值）
	Deleted       int      // 实际删除数（dry-run 为 0）
	Renamed       int      // 幸存文件改回原名的次数
	Reclaimed     int64    // 实际释放的本地存储字节数
	DryRun        bool     // 是否为预览模式
	Details       []string // 每组明细
	Warnings      []string // 汇总警告
}

// suffixNameRe 匹配 "xxx (1).jpg" / "xxx(1).jpg" / "xxx (2).png" 形式的副本命名
var suffixNameRe = regexp.MustCompile(`^(.+?) ?\(([0-9]+)\)(\.[^.]+)$`)

// nameSuffixBase 提取副本名去掉 "(N)" 后缀后的原文件名（如 "IMG_a (1).jpg" → "IMG_a.jpg"）
func nameSuffixBase(name string) (string, bool) {
	m := suffixNameRe.FindStringSubmatch(name)
	if m == nil {
		return "", false
	}
	return m[1] + m[3], true
}

// dupMeta JPEG 元数据探测结果
type dupMeta struct {
	jpeg        bool // 是否为 JPEG（只有 JPEG 需要/可以做元数据级核验）
	exifNonZero int  // EXIF 段内非零字节数（tag 被清零则显著偏低，用于挑 EXIF 更完整的一方）
}

// jpegScanEXIFTail 扫描 JPEG 段：返回 EXIF 段非零字节数、SOS（FFDA）起始下标。
// 从 SOS 起的熵编码数据 = 真实像素内容；元数据段（APPn）都在 SOS 之前，
// 因此两个 JPEG 从 SOS 起逐字节相等即可判定像素一致（无需解码）。
func jpegScanEXIFTail(data []byte) (exifNonZero int, sos int, ok bool) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, -1, false
	}
	i := 2
	for i+3 < len(data) {
		if data[i] != 0xFF {
			return 0, -1, false // 段对齐异常，非标准 JPEG
		}
		marker := data[i+1]
		if marker == 0xDA { // SOS：熵编码图像数据开始
			return exifNonZero, i, true
		}
		if marker == 0xD9 { // EOI：无图像数据
			return 0, -1, false
		}
		segLen := int(data[i+2])<<8 | int(data[i+3])
		if segLen < 2 || i+2+segLen > len(data) {
			return 0, -1, false
		}
		if marker == 0xE1 && segLen >= 8 && bytes.Equal(data[i+4:i+8], []byte("Exif")) {
			for _, b := range data[i+8 : i+2+segLen] {
				if b != 0 {
					exifNonZero++
				}
			}
		}
		i += 2 + segLen
	}
	return 0, -1, false
}

// readLocalPlainContent 读取文件在本地存储（provider_type=LOCAL、明文、整文件）上的内容。
// 返回 ok=false 表示无可用本地明文副本（无法离线核验）。
func readLocalPlainContent(hash string, localID uint32) ([]byte, bool) {
	if localID == 0 || hash == "" {
		return nil, false
	}
	locations, err := db.GetFileLocationsByBlake3Hash(hash)
	if err != nil || len(locations) == 0 {
		return nil, false
	}
	for _, loc := range locations {
		if loc.ProviderId == nil || *loc.ProviderId != localID {
			continue
		}
		if loc.Encrypted != nil && *loc.Encrypted {
			continue
		}
		if loc.Path == nil || *loc.Path == "" || *loc.Path == "/" {
			continue
		}
		if _, err := os.Stat(*loc.Path); err != nil {
			continue
		}
		data, err := os.ReadFile(*loc.Path)
		if err != nil {
			continue
		}
		return data, true
	}
	return nil, false
}

// suffixCandDecision 单个后缀候选的核验结论
type suffixCandDecision struct {
	cand     *entity.FileORM
	dup      bool    // 是否核验为重复（可删）
	reason   string  // dup 时的核验依据 / 非 dup 时的原因
	kind     string  // dup | diff-size | diff-content | skip
	meta     dupMeta // 候选自身的 JPEG 元数据（jpeg 核验路径有效）
	baseMeta dupMeta // 原文件（锚点）的 JPEG 元数据（jpeg 核验路径有效）
}

// verifySuffixCandidate 核验候选与原名文件是否实质重复（调用前提：尺寸相同且 hash 不同）
func verifySuffixCandidate(cand, base *entity.FileORM, localID uint32) *suffixCandDecision {
	d := &suffixCandDecision{cand: cand}
	candJpeg := isJpegFile(cand)
	baseJpeg := isJpegFile(base)

	candData, okC := readLocalPlainContent(strPtrVal(cand.Hash), localID)
	baseData, okB := readLocalPlainContent(strPtrVal(base.Hash), localID)
	if !okC || !okB {
		d.kind, d.reason = "skip", "无本地明文副本，无法核验"
		return d
	}

	// JPEG：比较 SOS 之后的像素数据 + 统计 EXIF 非零字节
	if candJpeg && baseJpeg {
		nzC, sosC, ok1 := jpegScanEXIFTail(candData)
		nzB, sosB, ok2 := jpegScanEXIFTail(baseData)
		d.meta = dupMeta{jpeg: true, exifNonZero: nzC}
		d.baseMeta = dupMeta{jpeg: true, exifNonZero: nzB}
		if !ok1 || !ok2 || sosC < 0 || sosB < 0 {
			d.kind, d.reason = "skip", "JPEG 结构异常，无法核验"
			return d
		}
		if bytes.Equal(candData[sosC:], baseData[sosB:]) {
			d.dup = true
			d.reason = "JPEG 像素数据一致（仅元数据/EXIF 差异）"
			d.kind = "dup"
			return d
		}
		d.kind, d.reason = "diff-content", "同尺寸但 JPEG 像素数据不同"
		return d
	}

	// 其他格式：整文件字节比较
	if bytes.Equal(candData, baseData) {
		d.dup = true
		d.kind, d.reason = "dup", "字节完全相同"
		return d
	}
	d.kind, d.reason = "diff-content", "同尺寸但内容不同"
	return d
}

// isJpegFile 按扩展名判断是否为 JPEG
func isJpegFile(f *entity.FileORM) bool {
	if f.Name == nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(*f.Name))
	return ext == ".jpg" || ext == ".jpeg"
}

// isImageFile 按扩展名判断是否为支持解码的图片格式（用于压缩衍生核验）
func isImageFile(f *entity.FileORM) bool {
	if f.Name == nil {
		return false
	}
	switch strings.ToLower(filepath.Ext(*f.Name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp", ".gif":
		return true
	}
	return false
}

// ============================================================================
// 压缩衍生核验：同名 "(N)" 配对、hash 不同、文件大小不同，但小图是大图
// 压缩/重编码衍生而来。把双方解码成像素，缩放到统一小尺寸灰度图后逐像素
// 比对，平均差异低于阈值即判定为衍生关系（可删小图保大图）。
// ============================================================================

// perceptualGridSize 感知比对网格边长（32x32=1024 像素，兼顾精度与性能）
const perceptualGridSize = 32

// perceptualDiffThreshold 平均像素差异阈值（0-255 灰度）。
// JPEG 重压缩/缩放引入的正常误差通常 < 8；两幅不同照片一般 > 20。
const perceptualDiffThreshold = 10.0

// perceptualGray 把图片数据解码并缩放为 n×n 灰度矩阵。
// 解码失败返回 nil（调用方回退为不可核验）。
func perceptualGray(data []byte, n int) [][]float64 {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		return nil
	}
	b := img.Bounds()
	out := make([][]float64, n)
	// 双线性采样：目标像素 (i,j) 映射回源图坐标取均值
	for j := 0; j < n; j++ {
		out[j] = make([]float64, n)
		sy0 := b.Min.Y + j*b.Dy()/n
		sy1 := b.Min.Y + (j+1)*b.Dy()/n
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for i := 0; i < n; i++ {
			sx0 := b.Min.X + i*b.Dx()/n
			sx1 := b.Min.X + (i+1)*b.Dx()/n
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var sum, cnt float64
			for y := sy0; y < sy1; y++ {
				for x := sx0; x < sx1; x++ {
					r, g, bl, _ := img.At(x, y).RGBA()
					// ITU-R BT.601 灰度权重（RGBA 是 16bit，右移 8 位到 0-255）
					sum += 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
					cnt++
				}
			}
			if cnt > 0 {
				out[j][i] = sum / cnt
			}
		}
	}
	return out
}

// perceptualSimilar 判断两图是否为压缩衍生关系。
// 灰度均值差 < 阈值 → 相似。同时返回平均差异值供日志/明细使用。
func perceptualSimilar(a, b [][]float64) (bool, float64) {
	if len(a) != len(b) || len(a) == 0 {
		return false, 0
	}
	var totalDiff float64
	var cnt int
	for j := range a {
		for i := range a[j] {
			diff := a[j][i] - b[j][i]
			if diff < 0 {
				diff = -diff
			}
			totalDiff += diff
			cnt++
		}
	}
	if cnt == 0 {
		return false, 0
	}
	avg := totalDiff / float64(cnt)
	return avg < perceptualDiffThreshold, avg
}

// verifyCompressedDerivative 对 尺寸不同 的同名图片对做压缩衍生核验。
// 返回 decision：衍生 → kind=compressed-dup（可删小图保大图）；
// 不相似 → diff-content；无法解码 → skip。
func verifyCompressedDerivative(cand, base *entity.FileORM, candData, baseData []byte) *suffixCandDecision {
	d := &suffixCandDecision{cand: cand}
	if !isImageFile(cand) || !isImageFile(base) {
		d.kind, d.reason = "diff-size", "同名但尺寸不同"
		return d
	}
	gC := perceptualGray(candData, perceptualGridSize)
	gB := perceptualGray(baseData, perceptualGridSize)
	if gC == nil || gB == nil {
		d.kind, d.reason = "skip", "图片解码失败，无法核验"
		return d
	}
	similar, avg := perceptualSimilar(gC, gB)
	if similar {
		d.dup = true
		d.kind = "compressed-dup"
		d.reason = fmt.Sprintf("压缩衍生（缩略像素平均差异 %.1f < %.0f）", avg, perceptualDiffThreshold)
		return d
	}
	d.kind, d.reason = "diff-content", fmt.Sprintf("同名不同尺寸且内容不相似（像素差异 %.1f）", avg)
	return d
}

// formatSuffixEXIFNote 生成 EXIF 保留策略说明（baseMeta 为原文件, candMeta 为候选副本）
func formatSuffixEXIFNote(baseMeta, candMeta dupMeta) string {
	b, c := baseMeta.exifNonZero, candMeta.exifNonZero
	switch {
	case b == 0 && c == 0:
		return "双方均无 EXIF"
	case b == 0:
		return "副本含 EXIF，原文件无"
	case c == 0:
		return "原文件含 EXIF，副本无"
	case b != c:
		return fmt.Sprintf("EXIF 完整度不同(非零 tag %d vs %d)，保留更完整方", b, c)
	default:
		return "EXIF 结构相同"
	}
}

// pickSuffixKeeper 从 原文件 + 核验通过的候选 中选出保留者：
// 1. 文件更大的一方优先（同名副本里更小的一方通常是压缩/重编码衍生，画质已损失）
// 2. JPEG 且 EXIF 完整度不同 → 保留非零 tag 更多的一方（拍摄时间等信息更全）
// 3. 否则保留无后缀的原名文件（名称干净）
// 4. 再同 → 保留最早创建，再同 → 保留 id 较小
func pickSuffixKeeper(base *entity.FileORM, baseMeta dupMeta, dupCands []*suffixCandDecision) *entity.FileORM {
	keeper := base
	kMeta := baseMeta
	for _, d := range dupCands {
		c := d.cand
		if betterSuffixKeeper(c, keeper, d.meta, kMeta) {
			keeper = c
			kMeta = d.meta
		}
	}
	return keeper
}

// betterSuffixKeeper 判断候选副本是否比当前保留者更该留下。
// 第一条即「同名带 (N) 后缀的副本不能只凭名称删除」：先比文件字节大小，
// 大的一方优先（更可能是原图，小的一方往往被压缩过、信息已丢）。
func betterSuffixKeeper(cand, cur *entity.FileORM, cMeta, curMeta dupMeta) bool {
	// 1. 文件更大的一方优先（覆盖压缩衍生对与 EXIF 补齐导致的大小差异）
	if uint64Val(cand.Size) != uint64Val(cur.Size) {
		return uint64Val(cand.Size) > uint64Val(cur.Size)
	}
	// 2. JPEG：EXIF 完整度不同时保留非零 tag 更多的一方（另一方 tag 被清零/丢失）
	if cMeta.jpeg && curMeta.jpeg && cMeta.exifNonZero != curMeta.exifNonZero {
		return cMeta.exifNonZero > curMeta.exifNonZero
	}
	// 3. 名字不带 (N) 后缀的优先（原名更干净）
	_, candSuffix := nameSuffixBase(strPtrVal(cand.Name))
	_, curSuffix := nameSuffixBase(strPtrVal(cur.Name))
	if candSuffix != curSuffix {
		return !candSuffix
	}
	// 4. 最早创建优先
	cTime := fileCreatedAt(cand)
	kTime := fileCreatedAt(cur)
	if !cTime.Equal(kTime) {
		return cTime.Before(kTime)
	}
	return strPtrVal(cand.Id) < strPtrVal(cur.Id)
}

// suffixRenamePlan 去重后幸存副本改名计划
type suffixRenamePlan struct{ id, newName string }

// suffixGroupOutcome 单个同名组的处理结果（待删记录、待改名、待回收 hash）
type suffixGroupOutcome struct {
	deleteIDs    []string
	deleteHashes []string
	renames      []suffixRenamePlan
}

// classifySuffixCandidate 判定单个候选是否构成重复：
// 尺寸不同→先比文件大小（大的一方优先保留），再做压缩衍生核验（图片可判衍生，
// 删小保大；非图片/不相似→diff-size/diff-content）；同 hash→dup；
// 同尺寸→读取本地内容核验（JPEG 像素/其他格式字节）
func classifySuffixCandidate(c, base *entity.FileORM, localID uint32) *suffixCandDecision {
	if !sameSize(base, c) {
		// 小图是大图压缩衍生而来（重编码/转格式），hash、文件大小都不同，
		// 但画面内容一致。解码双方做感知比对，确认衍生才删，删小保大。
		candData, okC := readLocalPlainContent(strPtrVal(c.Hash), localID)
		baseData, okB := readLocalPlainContent(strPtrVal(base.Hash), localID)
		if !okC || !okB {
			return &suffixCandDecision{cand: c, kind: "diff-size", reason: "同名但尺寸不同（无本地明文副本，无法核验）"}
		}
		return verifyCompressedDerivative(c, base, candData, baseData)
	}
	if sameHash(base, c) {
		return &suffixCandDecision{cand: c, dup: true, kind: "dup", reason: "内容 hash 相同"}
	}
	return verifySuffixCandidate(c, base, localID)
}

// processSuffixGroup 处理一个 原文件 + N 个后缀候选 的同名组：
// 逐个核验并累计统计，选出保留者，产出 删除/改名 计划
func processSuffixGroup(base *entity.FileORM, cands []*entity.FileORM, localID uint32, r *NameSuffixDedupResult) *suffixGroupOutcome {
	out := &suffixGroupOutcome{}
	var dupCands []*suffixCandDecision
	for _, c := range cands {
		d := classifySuffixCandidate(c, base, localID)
		switch d.kind {
		case "dup", "compressed-dup":
			dupCands = append(dupCands, d)
			if d.kind == "compressed-dup" {
				r.CompressedDup++
			} else {
				r.Duplicates++
			}
		case "diff-size":
			r.DiffSize++
		case "diff-content":
			r.DiffContent++
		case "skip":
			r.Skipped++
		}
		if d.kind != "dup" && d.kind != "compressed-dup" {
			appendSuffixDetail(r, base, d)
		}
	}
	if len(dupCands) == 0 {
		return out
	}

	// base 元数据：任一 jpeg 核验已读过原文件，EXIF 统计通用
	baseMeta := dupMeta{jpeg: isJpegFile(base)}
	for _, d := range dupCands {
		if d.meta.jpeg {
			baseMeta = d.baseMeta
			break
		}
	}
	keeper := pickSuffixKeeper(base, baseMeta, dupCands)
	for _, d := range dupCands {
		if strPtrVal(d.cand.Id) == strPtrVal(keeper.Id) {
			continue
		}
		out.deleteIDs = append(out.deleteIDs, strPtrVal(d.cand.Id))
		out.deleteHashes = append(out.deleteHashes, strPtrVal(d.cand.Hash))
	}
	if strPtrVal(base.Id) != strPtrVal(keeper.Id) {
		// 保留后缀副本（文件更大或 EXIF 更完整），原文件删除，副本改回原名
		r.KeepSuffix++
		if uint64Val(keeper.Size) > uint64Val(base.Size) {
			r.KeptLarger++
		}
		out.deleteIDs = append(out.deleteIDs, strPtrVal(base.Id))
		out.deleteHashes = append(out.deleteHashes, strPtrVal(base.Hash))
		out.renames = append(out.renames, suffixRenamePlan{id: strPtrVal(keeper.Id), newName: strPtrVal(base.Name)})
	}
	r.Details = append(r.Details, formatSuffixGroupDetail(base, keeper, dupCands))
	return out
}

// executeSuffixDedup 执行 删除冗余记录 → 改名 → 回收孤儿物理数据 → 清理孤儿上传任务
//
// 孤儿上传任务：被删的 file 记录原有关联的 upload_records（甚至还在 PENDING/UPLOADING
// 的 task），若不清理会持续被 dispatch 拉起又找不到源文件，造成
// "source file not found" / "duplicate key idx_ur_fh" 等噪音。安全起见：只清理
// 待删 hash 在 upload_records 中所有记录（不论 task_status）+ 关联的 chunks。
func executeSuffixDedup(deleteIDs []string, renames []suffixRenamePlan, deleteHashes []string) (int64, error) {
	if err := db.DeletePermanently(deleteIDs); err != nil {
		return 0, fmt.Errorf("删除冗余文件失败: %w", err)
	}
	if err := db.DeleteFileHistoryByFileIDs(deleteIDs); err != nil {
		llog.L().Warnf("dedup: 清理 file_histories 失败: %v", err)
	}
	// 清理孤儿上传任务（按 hash 删 upload_records + upload_chunk_records）
	if len(deleteHashes) > 0 {
		if n, err := cleanupUploadTasksByHashes(deleteHashes); err != nil {
			llog.L().Warnf("dedup: 清理 upload_records 失败: %v", err)
		} else if n > 0 {
			llog.L().Infof("dedup: cleaned %d orphan upload records", n)
		}
	}
	if len(renames) > 0 {
		renameObjs := make([]*native.RenameObj, 0, len(renames))
		for _, r := range renames {
			renameObjs = append(renameObjs, &native.RenameObj{Id: utils.StringPtr(r.id), NewName: utils.StringPtr(r.newName)})
		}
		if err := db.RenameFiles(renameObjs); err != nil {
			return 0, fmt.Errorf("改名失败: %w", err)
		}
	}
	var reclaimed int64
	for _, hash := range deleteHashes {
		got, err := ReclaimOrphanHashStorage(hash)
		if err != nil {
			llog.L().Warnf("dedup: 回收存储失败, hash: %s, err: %v", hash, err)
			continue
		}
		reclaimed += got
	}
	return reclaimed, nil
}

// cleanupUploadTasksByHashes 按 hash 清理 upload_records + upload_chunk_records
// 返回清理的 upload_records 数量
func cleanupUploadTasksByHashes(hashes []string) (int64, error) {
	if len(hashes) == 0 {
		return 0, nil
	}
	ctx := context.Background()
	// 先取出所有相关 upload_record id（带 chunks 清理）
	var ids []uint32
	if err := db.GetDB().WithContext(ctx).
		Model((*entity.UploadRecordORM)(nil)).
		Where("file_hash IN ?", hashes).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := db.GetDB().WithContext(ctx).
		Where("upload_id IN ?", ids).
		Delete((*entity.UploadChunkRecordORM)(nil)).Error; err != nil {
		return 0, err
	}
	if err := db.GetDB().WithContext(ctx).
		Where("id IN ?", ids).
		Delete((*entity.UploadRecordORM)(nil)).Error; err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

// DedupNameSuffixDuplicates 扫描 baseParentID 子树，按 同目录同名字 + "(N)" 后缀 配对，
// 先比文件大小（大的一方优先保留），再对尺寸相同的配对做内容核验
// （JPEG 比较像素数据/其他格式比较字节），确认实质重复后删除冗余记录；
// 冗余记录 hash 无任何引用（含回收站软删、历史）时，
// 回收其在所有存储提供者上的物理副本与缩略图。dryRun=true 只预览不执行。
func DedupNameSuffixDuplicates(baseParentID string, dryRun bool) (*NameSuffixDedupResult, error) {
	ownerID, err := dedupBaseOwnerID(baseParentID)
	if err != nil {
		return nil, err
	}
	files, err := collectSubtreeFiles(baseParentID, ownerID)
	if err != nil {
		return nil, err
	}
	result := &NameSuffixDedupResult{DryRun: dryRun, Scanned: len(files)}
	localID, _ := db.GetLocalProviderID() // 无本地 provider 时退化为仅同 hash 命中

	// 按父目录分组，组内文件名唯一（库中有 (owner,name,parent) 活跃唯一索引）
	byParent := make(map[string][]*entity.FileORM)
	for _, f := range files {
		if f.ParentId == nil {
			continue
		}
		byParent[*f.ParentId] = append(byParent[*f.ParentId], f)
	}

	var deleteIDs, deleteHashes []string
	var renames []suffixRenamePlan
	totalSuffix, consumed, processed := 0, 0, 0

	for _, group := range byParent {
		// 原文件（无后缀）作为锚点；后缀候选按 baseName 索引
		var plainFiles []*entity.FileORM
		candByBase := make(map[string][]*entity.FileORM)
		for _, f := range group {
			if base, ok := nameSuffixBase(strPtrVal(f.Name)); ok {
				candByBase[base] = append(candByBase[base], f)
				totalSuffix++
			} else {
				plainFiles = append(plainFiles, f)
			}
		}
		for _, baseFile := range plainFiles {
			cands := candByBase[strPtrVal(baseFile.Name)]
			if len(cands) == 0 {
				continue
			}
			result.Groups++
			out := processSuffixGroup(baseFile, cands, localID, result)
			deleteIDs = append(deleteIDs, out.deleteIDs...)
			deleteHashes = append(deleteHashes, out.deleteHashes...)
			renames = append(renames, out.renames...)
			consumed += len(cands)
			processed += len(cands)
			if processed%200 == 0 {
				llog.L().Infof("dedup: scanned %d/%d candidates", processed, totalSuffix)
			}
		}
	}
	result.Unpaired = totalSuffix - consumed
	result.ToDelete = len(deleteIDs)
	result.Renamed = len(renames)

	if dryRun || len(deleteIDs) == 0 {
		return result, nil
	}

	reclaimed, err := executeSuffixDedup(deleteIDs, renames, deleteHashes)
	if err != nil {
		return nil, err
	}
	result.Reclaimed = reclaimed
	result.Deleted = len(deleteIDs)
	llog.L().Infof("dedup: deleted %d duplicate file records (%d groups), reclaimed %d bytes",
		result.Deleted, result.Groups, result.Reclaimed)
	return result, nil
}

// sameSize 两文件记录 size 字段是否相同（nil 视为 0）
func sameSize(a, b *entity.FileORM) bool {
	return uint64Val(a.Size) == uint64Val(b.Size)
}

// sameHash 两文件记录 hash 是否均非空且相同
func sameHash(a, b *entity.FileORM) bool {
	ha, hb := strPtrVal(a.Hash), strPtrVal(b.Hash)
	return ha != "" && ha == hb
}

func uint64Val(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

// appendSuffixDetail 追加 非重复（需人工/无法核验）候选的明细行
func appendSuffixDetail(result *NameSuffixDedupResult, base *entity.FileORM, d *suffixCandDecision) {
	prefix := "需人工"
	if d.kind == "skip" {
		prefix = "跳过"
	}
	result.Details = append(result.Details, fmt.Sprintf("%s: %q 与 %q %s",
		prefix, truncateStr(strPtrVal(d.cand.Name), 40), truncateStr(strPtrVal(base.Name), 40), d.reason))
}

// formatSuffixGroupDetail 格式化一组确认重复的最终处理方案
func formatSuffixGroupDetail(base *entity.FileORM, keeper *entity.FileORM, dupCands []*suffixCandDecision) string {
	exifNote := ""
	verifyWay := dupCands[0].reason
	for _, d := range dupCands {
		if d.meta.jpeg {
			exifNote = "；" + formatSuffixEXIFNote(d.baseMeta, d.meta)
			verifyWay = d.reason
			break
		}
	}
	if strPtrVal(keeper.Id) == strPtrVal(base.Id) {
		delNames := make([]string, 0, len(dupCands))
		for _, d := range dupCands {
			delNames = append(delNames, fmt.Sprintf("%s (%s)",
				truncateStr(strPtrVal(d.cand.Name), 30), formatFileSize(uint64Val(d.cand.Size))))
		}
		return fmt.Sprintf("重复组: 保留 %q (%s), 删除副本 %d 个: %s [核验: %s%s]",
			truncateStr(strPtrVal(keeper.Name), 40), formatFileSize(uint64Val(keeper.Size)),
			len(delNames), strings.Join(delNames, ", "), verifyWay, exifNote)
	}
	// 保留带后缀的副本并改回原名（通常因副本文件更大或 EXIF 更完整）
	return fmt.Sprintf("重复组: 保留副本 %q (%s) 并改回原名 %q, 删除原文件 %q (%s) [核验: %s%s]",
		truncateStr(strPtrVal(keeper.Name), 40), formatFileSize(uint64Val(keeper.Size)),
		truncateStr(strPtrVal(base.Name), 40),
		truncateStr(strPtrVal(base.Name), 40), formatFileSize(uint64Val(base.Size)),
		verifyWay, exifNote)
}

// formatFileSize 字节数转人类可读大小（明细里标注「保大」的依据）
func formatFileSize(n uint64) string {
	const mb, kb = 1024 * 1024, 1024
	switch {
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.0f KB", float64(n)/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// reclaimDedupHashStorage 回收不再被任何记录引用的 hash 的全部物理数据：
//   - 引用检查（Unscoped 含回收站软删记录 + file_histories），有引用一律不动
//   - 对每个有副本的存储提供者按需懒加载驱动（storage.EnsureProvider），
//     经 batchDeleteFromProvider 删除物理副本：成功者删 file_locations 行并扣减容量，
//     失败者（驱动不可用/网络错误）标记 DELETED_FAILED 保留，待后续重试或服务端清理
//   - 删除按 hash 命名的缩略图缓存
//
// 返回实际删除的副本总字节数（本地 + 远程）。
//
// ReclaimOrphanHashStorage 回收一个 hash 在所有提供者上的物理副本。
// 安全前提（调用方保证）：该 hash 在 files / file_histories 中已无任何引用。
// 流程：查全部 file_locations → 按提供者分组 → 调驱动批量删除（失败标记
// DELETED_FAILED 待重试）→ 删 file_locations 行 → 扣减容量 → 删缩略图。
// 返回回收的字节数（各提供者合计）。
func ReclaimOrphanHashStorage(hash string) (int64, error) {
	if hash == "" {
		return 0, nil
	}
	refs, err := db.CountFilesByHashIncludingDeleted(hash)
	if err != nil {
		return 0, err
	}
	if refs > 0 {
		return 0, nil
	}
	histRefs, err := db.CountFileHistoriesByHash(hash)
	if err != nil {
		return 0, err
	}
	if histRefs > 0 {
		return 0, nil
	}

	locations, err := db.GetFileLocationsByBlake3Hash(hash)
	if err != nil {
		return 0, err
	}
	if len(locations) == 0 {
		return 0, nil
	}

	// 按 provider 分组
	byProvider := make(map[uint32][]*entity.FileLocationORM)
	for _, loc := range locations {
		if loc.ProviderId == nil {
			continue
		}
		byProvider[*loc.ProviderId] = append(byProvider[*loc.ProviderId], loc)
	}

	ctx := context.Background()
	var reclaimed int64
	for providerID, locs := range byProvider {
		drv, err := storage.EnsureProvider(providerID)
		if err != nil {
			llog.L().Warnf("dedup: provider %d 驱动不可用，副本保留待重试, hash: %s, err: %v", providerID, hash, err)
			continue
		}
		if _, ok := drv.(types.BatchDeleter); !ok {
			llog.L().Warnf("dedup: provider %d 不支持批量删除，副本保留, hash: %s", providerID, hash)
			continue
		}
		deletedIDs, failedIDs, deletedSize := batchDeleteFromProvider(ctx, providerID, locs)
		if len(failedIDs) > 0 {
			if err := db.UpdateFileLocationStatus(failedIDs, int32(native.LocationStatus_PDELETED_FAILED)); err != nil {
				llog.L().Warnf("dedup: 标记 DELETED_FAILED 失败: %v", err)
			} else {
				llog.L().Warnf("dedup: %d 个副本删除失败, 已标记 DELETED_FAILED, provider: %d, hash: %s", len(failedIDs), providerID, hash)
			}
		}
		if len(deletedIDs) > 0 {
			if err := db.DeleteFileLocations(deletedIDs); err != nil {
				return 0, err
			}
		}
		if deletedSize > 0 {
			if err := db.DecrementUsedCapacity(providerID, deletedSize); err != nil {
				llog.L().Warnf("dedup: 扣减容量失败, provider: %d, err: %v", providerID, err)
			}
			reclaimed += int64(deletedSize)
		}
	}
	// 缩略图按 hash 命名，hash 已无任何引用可安全删除
	if err := thumbnail.NewGenerator(filepath.Join(globals.DataDir, "thumbnails")).DeleteAllThumbnails(hash); err != nil {
		llog.L().Warnf("dedup: 删除缩略图失败, hash: %s, err: %v", hash, err)
	}
	return reclaimed, nil
}
