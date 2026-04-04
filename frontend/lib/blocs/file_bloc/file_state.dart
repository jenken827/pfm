part of 'file_bloc.dart';

/// 文件列表状态
enum FileStateStatus { initial, loading, success, failure }

/// 文件视图模式
enum FileViewMode { list, grid }

/// 文件状态
class FileState extends Equatable {
  const FileState({
    this.status = FileStateStatus.initial,
    this.msg = '',
    this.currentDirId = '',
    this.currentDirName = '',
    this.files = const [],
    this.selectedFiles = const [],
    this.sort = const FileSort(),
    this.isGridView = false,
    this.isSelectMode = false,
    this.breadcrumbs = const [],
    this.searchKeyword = '',
    this.storageProviders = const [],
  });

  final FileStateStatus status;
  final String msg;
  final String currentDirId;
  final String currentDirName;
  final List<File> files;
  final List<File> selectedFiles;
  final FileSort sort;
  final bool isGridView;
  final bool isSelectMode;
  final List<Breadcrumb> breadcrumbs;
  final String searchKeyword;
  final List<StorageProvider> storageProviders;

  @override
  List<Object?> get props => [
    status,
    msg,
    currentDirId,
    currentDirName,
    files,
    selectedFiles,
    sort,
    isGridView,
    isSelectMode,
    breadcrumbs,
    searchKeyword,
    storageProviders,
  ];

  FileState copyWith({
    FileStateStatus? status,
    String? msg,
    String? currentDirId,
    String? currentDirName,
    List<File>? files,
    List<File>? selectedFiles,
    FileSort? sort,
    bool? isGridView,
    bool? isSelectMode,
    List<Breadcrumb>? breadcrumbs,
    String? searchKeyword,
    List<StorageProvider>? storageProviders,
  }) {
    return FileState(
      status: status ?? this.status,
      msg: msg ?? this.msg,
      currentDirId: currentDirId ?? this.currentDirId,
      currentDirName: currentDirName ?? this.currentDirName,
      files: files ?? this.files,
      selectedFiles: selectedFiles ?? this.selectedFiles,
      sort: sort ?? this.sort,
      isGridView: isGridView ?? this.isGridView,
      isSelectMode: isSelectMode ?? this.isSelectMode,
      breadcrumbs: breadcrumbs ?? this.breadcrumbs,
      searchKeyword: searchKeyword ?? this.searchKeyword,
      storageProviders: storageProviders ?? this.storageProviders,
    );
  }
}

/// 面包屑
class Breadcrumb {
  final String id;
  final String name;

  const Breadcrumb({required this.id, required this.name});
}
