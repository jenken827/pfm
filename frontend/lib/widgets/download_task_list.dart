import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:open_file/open_file.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/utils/format.dart';

/// 下载任务列表
class DownloadTaskList extends StatelessWidget {
  const DownloadTaskList({super.key});

  @override
  Widget build(BuildContext context) {
    return BlocBuilder<TransferBloc, TransferState>(
      buildWhen: (previous, current) =>
          previous.downloadTasks != current.downloadTasks,
      builder: (context, state) {
        final tasks = state.downloadTasks;

        if (tasks.isEmpty) {
          return _buildEmptyState(context);
        }

        return ListView.builder(
          padding: const EdgeInsets.symmetric(vertical: 8),
          itemCount: tasks.length,
          itemBuilder: (context, index) {
            return _DownloadTaskItem(task: tasks[index]);
          },
        );
      },
    );
  }

  Widget _buildEmptyState(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            Icons.cloud_download_outlined,
            size: 64,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          const SizedBox(height: 16),
          Text(
            al.downloadTask_noTasks,
            style: Theme.of(context).textTheme.titleMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            al.downloadTask_noTasksHint,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}

/// 单个下载任务项
class _DownloadTaskItem extends StatelessWidget {
  const _DownloadTaskItem({required this.task});

  final DownloadTaskState task;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;

    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // 文件名和状态
            Row(
              children: [
                Icon(
                  Icons.insert_drive_file,
                  color: colorScheme.primary,
                  size: 24,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        task.fileName,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.titleSmall,
                      ),
                      const SizedBox(height: 4),
                      Text(
                        formatCapacityString(task.fileSize),
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                ),
                _buildStatusChip(context),
                // 操作菜单
                PopupMenuButton<String>(
                  icon: const Icon(Icons.more_vert, size: 20),
                  onSelected: (value) => _handleMenuAction(context, value),
                  itemBuilder: (context) {
                    final al = AppLocalizations.of(context)!;
                    return _buildMenuItems(al);
                  },
                ),
              ],
            ),
            // 下载路径（已完成时显示）
            if (task.isCompleted && task.localPath != null) ...[
              const SizedBox(height: 8),
              InkWell(
                onTap: () => _copyPath(context),
                borderRadius: BorderRadius.circular(4),
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 4),
                  child: Row(
                    children: [
                      Icon(
                        Icons.folder_outlined,
                        size: 14,
                        color: colorScheme.onSurfaceVariant,
                      ),
                      const SizedBox(width: 4),
                      Expanded(
                        child: Text(
                          task.localPath!,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color: colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                      Icon(
                        Icons.copy,
                        size: 14,
                        color: colorScheme.onSurfaceVariant,
                      ),
                    ],
                  ),
                ),
              ),
            ],
            // 进度条
            if (task.isDownloading || task.isPaused) ...[
              const SizedBox(height: 12),
              LinearProgressIndicator(
                value: task.progress / 100,
                backgroundColor: colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(4),
                minHeight: 6,
              ),
              const SizedBox(height: 8),
              Row(
                children: [
                  Text(
                    '${formatCapacityString(task.downloadedSize)} / ${formatCapacityString(task.fileSize)}',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const Spacer(),
                  Text(
                    '${task.progress.toStringAsFixed(1)}%',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ],
            // 错误信息（失败或预下载状态）
            if ((task.isFailed || task.isPreDownload) &&
                task.errorMessage != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  task.errorMessage!,
                  style: Theme.of(
                    context,
                  ).textTheme.bodySmall?.copyWith(color: colorScheme.error),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
          ],
        ),
      ),
    );
  }

  List<PopupMenuItem<String>> _buildMenuItems(AppLocalizations al) {
    return [
      if (task.isDownloading)
        PopupMenuItem(value: 'pause', child: Text(al.downloadTask_pause)),
      if (task.isPaused)
        PopupMenuItem(value: 'resume', child: Text(al.downloadTask_resume)),
      if (task.isFailed || task.isPreDownload)
        PopupMenuItem(value: 'retry', child: Text(al.downloadTask_retry)),
      if (task.isDownloading || task.isPaused)
        PopupMenuItem(value: 'cancel', child: Text(al.downloadTask_cancel)),
      if (task.isCompleted && task.localPath != null) ...[
        PopupMenuItem(value: 'open', child: Text(al.downloadTask_openFile)),
        PopupMenuItem(value: 'folder', child: Text(al.downloadTask_openDir)),
      ],
      PopupMenuItem(value: 'remove', child: Text(al.common_remove)),
    ];
  }

  void _handleMenuAction(BuildContext context, String value) {
    final bloc = context.read<TransferBloc>();
    switch (value) {
      case 'pause':
        bloc.add(TransferPauseEvent(task.taskId));
        break;
      case 'resume':
        bloc.add(TransferResumeEvent(task.taskId));
        break;
      case 'retry':
        bloc.add(TransferRetryEvent(task.taskId));
        break;
      case 'cancel':
        bloc.add(TransferCancelEvent(task.taskId));
        break;
      case 'remove':
        bloc.add(TransferRemoveEvent(task.taskId));
        break;
      case 'open':
        _openFile(context);
        break;
      case 'folder':
        _openFolder(context);
        break;
    }
  }

  Future<void> _openFile(BuildContext context) async {
    final al = AppLocalizations.of(context)!;
    if (task.localPath == null) return;
    
    final file = File(task.localPath!);
    if (!await file.exists()) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.downloadTask_fileNotExist)),
        );
      }
      return;
    }

    final result = await OpenFile.open(task.localPath!);
    if (result.type != ResultType.done && context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('${al.downloadTask_cannotOpenFile}: ${result.message}')),
      );
    }
  }

  Future<void> _openFolder(BuildContext context) async {
    final al = AppLocalizations.of(context)!;
    if (task.localPath == null) return;
    
    final file = File(task.localPath!);
    if (!await file.exists()) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.downloadTask_fileNotExist)),
        );
      }
      return;
    }

    // Android: 使用 open_file 打开文件管理器并定位到文件
    // iOS: 不支持直接打开目录，只能打开文件
    if (Platform.isAndroid) {
      final result = await OpenFile.open(task.localPath!);
      if (result.type != ResultType.done && context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.downloadTask_cannotOpenDir}: ${result.message}')),
        );
      }
    } else {
      // iOS 不支持打开目录，复制路径提示用户
      _copyPath(context);
    }
  }

  void _copyPath(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    if (task.localPath == null) return;
    
    Clipboard.setData(ClipboardData(text: task.localPath!));
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(al.downloadTask_pathCopied)),
    );
  }

  Widget _buildStatusChip(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    String label;
    Color color;

    switch (task.status) {
      case DownloadTaskStatus.pending:
        label = al.downloadTask_statusWaiting;
        color = Colors.grey;
        break;
      case DownloadTaskStatus.downloading:
        label = al.downloadTask_statusDownloading;
        color = Theme.of(context).colorScheme.primary;
        break;
      case DownloadTaskStatus.completed:
        label = al.downloadTask_statusCompleted;
        color = Colors.green;
        break;
      case DownloadTaskStatus.paused:
        label = al.downloadTask_statusPaused;
        color = Colors.orange;
        break;
      case DownloadTaskStatus.cancelled:
        label = al.downloadTask_statusCanceled;
        color = Colors.grey;
        break;
      case DownloadTaskStatus.failed:
        label = al.downloadTask_statusFailed;
        color = Theme.of(context).colorScheme.error;
        break;
      case DownloadTaskStatus.preDownload:
        label = al.downloadTask_statusPreDownload;
        color = Colors.blue;
        break;
    }

    return Container(
      margin: const EdgeInsets.only(right: 8),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: color.withValues(alpha: 0.3)),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 11,
          color: color,
          fontWeight: FontWeight.w500,
        ),
      ),
    );
  }
}
