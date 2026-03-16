import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../pages/pages.dart';

part 'files_route.g.dart';

/// 文件列表路由
@TypedGoRoute<FileListRoute>(path: '/files')
@immutable
class FileListRoute extends GoRouteData with $FileListRoute {
  const FileListRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const FilePage();
  }
}
