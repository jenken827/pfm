import 'package:equatable/equatable.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/proto/native/provider.pb.dart';
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
    on<FileLoadMoreEvent>(_onLoadMore);
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
    on<FileLoadStorageProvidersEvent>(_onLoadStorageProviders);
  }

  final FmRepository fmRepository;

  /// 获取文件列表
  Future<void> _onFileList(FileListEvent event, Emitter<FileState> emit) async {
    emit(state.copyWith(status: FileStateStatus.loading));
    getLogger().info('get file list dir id: ${event.dirId}');
    final resp = await fmRepository.getFileList(parentId: event.dirId);

    if (!resp.success) {
      emit(
        state.copyWith(
          status: FileStateStatus.failure,
          msg: resp.message ?? '获取文件列表失败',
        ),
      );
      return;
    }
    final files = resp.data?.files.toList() ?? [];
    final total = resp.data?.total ?? 0;
    final hasMore = resp.data?.hasMore ?? false;
    // 应用排序
    state.sort.sort(files);

    final breadcrumb = _buildBreadcrumbs(event.dirId, event.dirName);
    emit(
      state.copyWith(
        status: FileStateStatus.success,
        currentDirId: event.dirId,
        currentDirName: event.dirName,
        files: files,
        breadcrumbs: breadcrumb,
        selectedFiles: [],
        isSelectMode: false,
        hasMore: hasMore,
        offset: files.length,
        total: total,
      ),
    );
  }

  /// 刷新文件列表和存储提供者列表
  Future<void> _onFileRefresh(
    FileRefreshEvent event,
    Emitter<FileState> emit,
  ) async {
    // 同时刷新文件列表和存储提供者列表
    add(FileListEvent(dirId: state.currentDirId, dirName: state.currentDirName));
    add(const FileLoadStorageProvidersEvent());
  }

  /// 加载更多文件
  Future<void> _onLoadMore(
    FileLoadMoreEvent event,
    Emitter<FileState> emit,
  ) async {
    // 如果没有更多数据或正在加载，直接返回
    if (!state.hasMore || state.status == FileStateStatus.loadingMore) {
      return;
    }

    emit(state.copyWith(status: FileStateStatus.loadingMore));
    getLogger().info('load more files, offset: ${state.offset}');

    final resp = await fmRepository.getFileList(
      parentId: state.currentDirId,
      offset: state.offset,
    );

    if (!resp.success) {
      emit(
        state.copyWith(
          status: FileStateStatus.failure,
          msg: resp.message ?? '加载更多失败',
        ),
      );
      return;
    }

    final newFiles = resp.data?.files.toList() ?? [];
    final hasMore = resp.data?.hasMore ?? false;
    final allFiles = [...state.files, ...newFiles];
    // 应用排序
    state.sort.sort(allFiles);

    emit(
      state.copyWith(
        status: FileStateStatus.success,
        files: allFiles,
        hasMore: hasMore,
        offset: allFiles.length,
      ),
    );
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
      parentId: state.currentDirId,
      enableEncryption: event.enableEncryption,
      providerIds: event.providerIds,
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
    add(FileListEvent(dirId: event.dirId, dirName: event.dirName));
  }

  /// 搜索
  Future<void> _onSearch(FileSearchEvent event, Emitter<FileState> emit) async {
    // TODO: 实现搜索功能
    emit(state.copyWith(searchKeyword: event.keyword));
  }

  /// 加载存储提供者列表
  Future<void> _onLoadStorageProviders(
    FileLoadStorageProvidersEvent event,
    Emitter<FileState> emit,
  ) async {
    final resp = await fmRepository.listStorageProviders();
    if (resp.success && resp.data != null) {
      emit(state.copyWith(storageProviders: resp.data!.providers.toList()));
    }
  }

  //有两种情况：1. 从目录进入  2. 从面包屑跳转
  List<Breadcrumb> _buildBreadcrumbs(String dirId, String dirName) {
    if (dirId.isEmpty) {
      return [Breadcrumb(name: '根目录', id: '')];
    }
    int i = 0;
    for (; i < state.breadcrumbs.length; i++) {
      if (state.breadcrumbs[i].id == dirId) {
        break;
      }
    }
    if (i == state.breadcrumbs.length) {
      return [...state.breadcrumbs, Breadcrumb(name: dirName, id: dirId)];
    }

    return state.breadcrumbs.sublist(0, i + 1);
  }
}
