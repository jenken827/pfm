import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../pages/pages.dart';
import '../proto/native/file.pb.dart';

part 'files_route.g.dart';

/// 文件列表路由
@TypedGoRoute<FileListRoute>(
  path: '/files',
  routes: [
    TypedGoRoute<FilePreviewRoute>(path: 'preview'),
    TypedGoRoute<FileDetailRoute>(path: 'detail'),
    TypedGoRoute<FolderPickerRoute>(path: 'picker'),
    TypedGoRoute<TrashRoute>(path: 'trash'),
    TypedGoRoute<FileTypeRoute>(path: 'type/:fileType'),
  ],
)
@immutable
class FileListRoute extends GoRouteData with $FileListRoute {
  const FileListRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const FilePage();
  }
}

/// 文件预览路由
@immutable
class FilePreviewRoute extends GoRouteData with $FilePreviewRoute {
  const FilePreviewRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    // 支持 PreviewArgs（带图片列表）或单 File 对象
    final extra = state.extra;
    if (extra is PreviewArgs) {
      return FilePreviewPage(
        file: extra.file,
        images: extra.images,
        initialIndex: extra.initialIndex,
      );
    }
    final file = extra! as File;
    return FilePreviewPage(file: file);
  }
}

/// 文件详情路由
@immutable
class FileDetailRoute extends GoRouteData with $FileDetailRoute {
  const FileDetailRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    final file = state.extra! as File;
    return FileDetailPage(file: file);
  }
}

/// 文件夹选择路由
@immutable
class FolderPickerRoute extends GoRouteData with $FolderPickerRoute {
  const FolderPickerRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    final extra = state.extra as Map<String, dynamic>?;
    final initialFolderId = extra?['initialFolderId'] as String? ?? '0';
    final excludeIds = (extra?['excludeIds'] as List<String>?) ?? [];
    return FolderPickerPage(
      initialFolderId: initialFolderId,
      excludeIds: excludeIds,
    );
  }
}

/// 回收站路由
@immutable
class TrashRoute extends GoRouteData with $TrashRoute {
  const TrashRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const TrashPage();
  }
}

/// 按文件类型浏览路由
@immutable
class FileTypeRoute extends GoRouteData with $FileTypeRoute {
  final int fileType;
  const FileTypeRoute({required this.fileType});

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return FileTypePage(fileType: fileType);
  }
}
