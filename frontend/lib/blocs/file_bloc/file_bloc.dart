import 'package:equatable/equatable.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/tables.pb.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/utils.dart';

part 'file_event.dart';
part 'file_state.dart';

/// 文件列表 Bloc
class FileBloc extends Bloc<FileEvent, FileState> {
  FileBloc(this.fmRepository) : super(const FileState()) {
    // on<FileServerSubScriptionEvent>(_onFileServerSubscription);
    on<FileListEvent>(_onFileList);
    on<FileRefreshEvent>(_onFileRefresh);
    on<FileViewModeEvent>(_onViewMode);
    on<FileSortEvent>(_onSort);
    on<FileSelectModeEvent>(_onSelectMode);
    on<FileSelectEvent>(_onSelect);
    on<FileSelectAllEvent>(_onSelectAll);
    on<FileCreateDirEvent>(_onCreateDir);
    on<FileDeleteEvent>(_onDelete);
    on<FileRenameEvent>(_onRename);
    on<FileNavigateEvent>(_onNavigate);
    on<FileSearchEvent>(_onSearch);
  }

  final FmRepository fmRepository;
  final _ls = GetIt.I.get<LocalStorage>();

  // // 监听文件服务器变化
  // Future<void> _onFileServerSubscription(
  //   FileServerSubScriptionEvent event,
  //   Emitter<FileState> emit,
  // ) async {
  //   emit.forEach(
  //     fmRepository.baseUrl,
  //     onData: (value) {
  //       if (value != null) {
  //         add(FileListEvent(state.currentDirId));
  //       }
  //       return state;
  //     },
  //   );
  // }

  /// 获取文件列表
  Future<void> _onFileList(FileListEvent event, Emitter<FileState> emit) async {
    emit(state.copyWith(status: FileStateStatus.loading));
    getLogger().info('get file list dir id: ${event.dirId}');
    final resp = await fmRepository.getFileList(parentId: event.dirId);

    if (resp.success) {
      final files = resp.data?.files.toList() ?? [];
      // 应用排序
      state.sort.sort(files);

      emit(
        state.copyWith(
          status: FileStateStatus.success,
          currentDirId: event.dirId,
          currentDirName: _getDirName(event.dirId, files),
          files: files,
          breadcrumbs: _buildBreadcrumbs(event.dirId),
          selectedFiles: [],
          isSelectMode: false,
        ),
      );
    } else {
      emit(
        state.copyWith(
          status: FileStateStatus.failure,
          msg: resp.message ?? '获取文件列表失败',
        ),
      );
    }
  }

  /// 刷新文件列表
  Future<void> _onFileRefresh(
    FileRefreshEvent event,
    Emitter<FileState> emit,
  ) async {
    add(FileListEvent(state.currentDirId));
  }

  /// 切换视图模式
  void _onViewMode(FileViewModeEvent event, Emitter<FileState> emit) {
    emit(state.copyWith(isGridView: event.isGridView));
  }

  /// 切换排序方式
  void _onSort(FileSortEvent event, Emitter<FileState> emit) {
    final sortedFiles = List<File>.from(state.files);
    event.sort.sort(sortedFiles);
    emit(state.copyWith(sort: event.sort, files: sortedFiles));
  }

  /// 切换多选模式
  void _onSelectMode(FileSelectModeEvent event, Emitter<FileState> emit) {
    emit(
      state.copyWith(
        isSelectMode: !state.isSelectMode,
        selectedFiles: state.isSelectMode ? [] : state.selectedFiles,
      ),
    );
  }

  /// 选中/取消选中文件
  void _onSelect(FileSelectEvent event, Emitter<FileState> emit) {
    final selectedFiles = List<File>.from(state.selectedFiles);
    if (selectedFiles.contains(event.file)) {
      selectedFiles.remove(event.file);
    } else {
      selectedFiles.add(event.file);
    }
    emit(state.copyWith(selectedFiles: selectedFiles));
  }

  /// 全选/取消全选
  void _onSelectAll(FileSelectAllEvent event, Emitter<FileState> emit) {
    if (state.selectedFiles.length == state.files.length) {
      emit(state.copyWith(selectedFiles: []));
    } else {
      emit(state.copyWith(selectedFiles: List.from(state.files)));
    }
  }

  /// 创建目录
  Future<void> _onCreateDir(
    FileCreateDirEvent event,
    Emitter<FileState> emit,
  ) async {
    emit(state.copyWith(status: FileStateStatus.loading));

    final resp = await fmRepository.createDir(
      name: event.name,
      parentId: state.currentDirId.isEmpty ? null : state.currentDirId,
    );

    if (resp.success) {
      // 刷新列表
      add(FileRefreshEvent());
    } else {
      emit(
        state.copyWith(
          status: FileStateStatus.failure,
          msg: resp.message ?? '创建目录失败',
        ),
      );
    }
  }

  /// 删除文件
  Future<void> _onDelete(FileDeleteEvent event, Emitter<FileState> emit) async {
    emit(state.copyWith(status: FileStateStatus.loading));

    final resp = await fmRepository.deleteFile(event.ids);

    if (resp.success) {
      // 刷新列表
      add(FileRefreshEvent());
    } else {
      emit(
        state.copyWith(
          status: FileStateStatus.failure,
          msg: resp.message ?? '删除失败',
        ),
      );
    }
  }

  /// 重命名文件
  Future<void> _onRename(FileRenameEvent event, Emitter<FileState> emit) async {
    emit(state.copyWith(status: FileStateStatus.loading));

    final resp = await fmRepository.renameFile([
      RenameObj()
        ..id = event.id
        ..newName = event.newName,
    ]);

    if (resp.success) {
      // 刷新列表
      add(FileRefreshEvent());
    } else {
      emit(
        state.copyWith(
          status: FileStateStatus.failure,
          msg: resp.message ?? '重命名失败',
        ),
      );
    }
  }

  /// 导航到目录
  void _onNavigate(FileNavigateEvent event, Emitter<FileState> emit) {
    add(FileListEvent(event.dirId));
  }

  /// 搜索
  Future<void> _onSearch(FileSearchEvent event, Emitter<FileState> emit) async {
    // TODO: 实现搜索功能
    emit(state.copyWith(searchKeyword: event.keyword));
  }

  String _getDirName(String? dirId, List<File> files) {
    if (dirId == null || dirId.isEmpty) {
      return '根目录';
    }
    final dir = files.where((f) => f.id == dirId).firstOrNull;
    return dir?.name ?? '未知目录';
  }

  List<Breadcrumb> _buildBreadcrumbs(String? dirId) {
    if (dirId == null || dirId.isEmpty) {
      return [const Breadcrumb(id: '', name: '根目录')];
    }
    // 简化版本，只显示当前目录
    return [
      const Breadcrumb(id: '', name: '根目录'),
      Breadcrumb(id: dirId, name: state.currentDirName),
    ];
  }
}
