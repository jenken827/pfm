// 单个文件的预览分流判定。
//
// 单独成模块是因为它被预览页和详情页同时用到：详情页要在"能预览"时才显示预览
// 按钮，而预览页拿它决定渲染哪条链路。规则必须与客户端
// `frontend/lib/pages/files/preview_router.dart` 的 buildPreviewPage 完全一致。

import { isTextPreviewable } from '../text_preview.js';

export function previewKindOf(file) {
  switch (file?.type) {
    case 'IMAGE':
      return 'image';
    case 'VIDEO':
    case 'AUDIO':
      return 'media';
    default:
      break;
  }
  if (isTextPreviewable(file)) return 'text';
  if (file?.type === 'DOCUMENT' || file?.type === 'ARCHIVE') return 'document';
  return 'detail';
}
