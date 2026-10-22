import 'package:flutter/widgets.dart';
import 'package:pfm/pages/files/file_detail_page.dart';
import 'package:pfm/pages/files/file_preview_page.dart';
import 'package:pfm/pages/files/flyfish_viewer_page.dart';
import 'package:pfm/pages/files/text_preview_page.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/utils/text_preview.dart';

/// 该文件是否有专门的预览页面（false 表示只能进文件详情页）
bool hasPreviewPage(File file) {
  switch (file.type) {
    case FileType.IMAGE:
    case FileType.VIDEO:
    case FileType.AUDIO:
    case FileType.DOCUMENT:
    case FileType.ARCHIVE:
      return true;
    default:
      return isTextPreviewable(file);
  }
}

/// 决定"打开这个文件"用哪个页面。
///
/// 预览入口只保留这一处判断，路由 builder、文件列表点击、搜索结果都复用，
/// 避免同一种文件在不同入口落到不同页面。分流顺序：
///
/// 1. 图片 / 音视频 → Dart 原生预览（`FilePreviewPage`）；
/// 2. 纯文本类（配置 / 代码 / 日志 / Markdown）→ Dart 文本预览；
/// 3. Office / PDF / 压缩包等 → Flyfish Viewer（WebView）；
/// 4. 其余 → 文件详情页。
Widget buildPreviewPage(File file, {List<File>? images, int initialIndex = 0}) {
  switch (file.type) {
    case FileType.IMAGE:
      return FilePreviewPage(
        file: file,
        images: images,
        initialIndex: initialIndex,
      );
    case FileType.VIDEO:
    case FileType.AUDIO:
      return FilePreviewPage(file: file);
    default:
      break;
  }

  if (isTextPreviewable(file)) {
    return TextPreviewPage(file: file);
  }

  if (file.type == FileType.DOCUMENT || file.type == FileType.ARCHIVE) {
    return FlyfishViewerPage(file: file);
  }

  return FileDetailPage(file: file);
}
