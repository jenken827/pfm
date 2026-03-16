part of 'file_bloc.dart';

/// 文件相关事件基类
sealed class FileEvent {
  const FileEvent();
}

class FileServerSubScriptionEvent extends FileEvent {
  const FileServerSubScriptionEvent();
}

/// 获取文件列表事件
class FileListEvent extends FileEvent {
  const FileListEvent(this.dirId);

  final String dirId;
}

/// 刷新文件列表事件
class FileRefreshEvent extends FileEvent {
  const FileRefreshEvent();
}

/// 切换视图模式事件（列表/网格）
class FileViewModeEvent extends FileEvent {
  const FileViewModeEvent(this.isGridView);

  final bool isGridView;
}

/// 切换排序方式事件
class FileSortEvent extends FileEvent {
  const FileSortEvent(this.sort);

  final FileSort sort;
}

/// 切换多选模式事件
class FileSelectModeEvent extends FileEvent {
  const FileSelectModeEvent();
}

/// 选中/取消选中文件事件
class FileSelectEvent extends FileEvent {
  const FileSelectEvent(this.file);

  final File file;
}

/// 全选/取消全选事件
class FileSelectAllEvent extends FileEvent {
  const FileSelectAllEvent();
}

/// 创建目录事件
class FileCreateDirEvent extends FileEvent {
  const FileCreateDirEvent(this.name);

  final String name;
}

/// 删除文件事件
class FileDeleteEvent extends FileEvent {
  const FileDeleteEvent(this.ids);

  final List<String> ids;
}

/// 重命名文件事件
class FileRenameEvent extends FileEvent {
  const FileRenameEvent(this.id, this.newName);

  final String id;
  final String newName;
}

/// 导航到目录事件
class FileNavigateEvent extends FileEvent {
  const FileNavigateEvent(this.dirId, this.dirName);

  final String dirId;
  final String dirName;
}

/// 搜索事件
class FileSearchEvent extends FileEvent {
  const FileSearchEvent(this.keyword);

  final String keyword;
}
