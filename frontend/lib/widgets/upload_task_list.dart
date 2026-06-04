import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:intl/intl.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/utils/format.dart';
import 'package:pfm/utils/platform.dart';

/// 上传任务列表
class UploadTaskList extends StatelessWidget {
  const UploadTaskList({super.key});

  @override
  Widget build(BuildContext context) {
    return BlocBuilder<TransferBloc, TransferState>(
      buildWhen: (previous, current) =>
          previous.uploadTasks != current.uploadTasks,
      builder: (context, state) {
        final tasks = state.uploadTasks;

        if (tasks.isEmpty) {
          return _buildEmptyState(context);
        }

        return Column(
          children: [
            // 操作栏
            _buildActionBar(context, state),
            // 任务列表
            Expanded(
              child: ListView.builder(
                padding: const EdgeInsets.symmetric(vertical: 8),
                itemCount: tasks.length,
                itemBuilder: (context, index) {
                  return _UploadTaskItem(task: tasks[index]);
                },
              ),
            ),
          ],
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
            Icons.cloud_upload_outlined,
            size: 64,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          const SizedBox(height: 16),
          Text(
            al.uploadTask_noTasks,
            style: Theme.of(context).textTheme.titleMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            al.uploadTask_noTasksHint,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildActionBar(BuildContext context, TransferState state) {
    final al = AppLocalizations.of(context)!;
    // 真正在上传中的数量（不含 PENDING）
    final activeCount = state.dbUploadingCount;
    final pendingCount = state.dbPendingCount;
    final completedCount = state.dbCompletedCount;
    final failedCount = state.dbFailedCount;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLow,
        border: Border(
          bottom: BorderSide(
            color: Theme.of(context).colorScheme.outlineVariant,
          ),
        ),
      ),
      child: Row(
        children: [
          // 状态统计（紧凑显示）
          if (activeCount > 0)
            _buildStatusChip(
              context,
              al.uploadTask_uploading(activeCount),
              Theme.of(context).colorScheme.primary,
            ),
          if (pendingCount > 0)
            _buildStatusChip(context, al.uploadTask_waiting(pendingCount), Colors.grey),
          if (failedCount > 0)
            _buildStatusChip(context, al.uploadTask_failed(failedCount), Colors.red),
          if (completedCount > 0)
            _buildStatusChip(context, al.uploadTask_completed(completedCount), Colors.green),
          const Spacer(),
          // 操作菜单
          PopupMenuButton<String>(
            icon: const Icon(Icons.more_vert, size: 20),
            onSelected: (value) {
              final bloc = context.read<TransferBloc>();
              switch (value) {
                case 'pause_all':
                  bloc.add(TransferPauseEvent(null));
                  break;
                case 'resume_all':
                  bloc.add(TransferResumeEvent(null));
                  break;
                case 'retry_all':
                  bloc.add(TransferRetryAllFailedEvent());
                  break;
                case 'clear_completed':
                  bloc.add(TransferClearCompletedEvent());
                  break;
              }
            },
            itemBuilder: (context) => [
              if (activeCount > 0)
                PopupMenuItem(
                  value: 'pause_all',
                  child: ListTile(
                    leading: const Icon(Icons.pause),
                    title: Text(al.uploadTask_pauseAll),
                    dense: true,
                  ),
                ),
              if (pendingCount > 0 || state.pausedUploadCount > 0)
                PopupMenuItem(
                  value: 'resume_all',
                  child: ListTile(
                    leading: const Icon(Icons.play_arrow),
                    title: Text(al.uploadTask_resumeAll),
                    dense: true,
                  ),
                ),
              if (failedCount > 0)
                PopupMenuItem(
                  value: 'retry_all',
                  child: ListTile(
                    leading: const Icon(Icons.refresh),
                    title: Text(al.uploadTask_retryFailed),
                    dense: true,
                  ),
                ),
              if (completedCount > 0 || failedCount > 0)
                PopupMenuItem(
                  value: 'clear_completed',
                  child: ListTile(
                    leading: const Icon(Icons.clear_all),
                    title: Text(al.uploadTask_clearCompleted),
                    dense: true,
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildStatusChip(BuildContext context, String label, Color color) {
    return Container(
      margin: const EdgeInsets.only(right: 6),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(12),
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

/// 单个上传任务项
class _UploadTaskItem extends StatelessWidget {
  const _UploadTaskItem({required this.task});

  final UploadTaskRecord task;

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
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
                // 文件图标
                Icon(_getFileIcon(), color: colorScheme.primary, size: 24),
                const SizedBox(width: 12),
                // 文件名
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        task.filename,
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
                // 状态
                _buildStatusChip(context),
                // 操作菜单
                _buildActions(context),
              ],
            ),
            const SizedBox(height: 12),
            LinearProgressIndicator(
              value: task.progress / 100,
              backgroundColor: colorScheme.surfaceContainerHighest,
              color: colorScheme.primary,
              borderRadius: BorderRadius.circular(4),
              minHeight: 6,
            ),
            const SizedBox(height: 8),
            // 进度条
            if (task.status == UploadStatus.HASHING) ...[
              Row(
                children: [
                  Text(
                    al.uploadTask_computingHash,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const Spacer(),
                  // Text(
                  //   '${task.hashProgress.toStringAsFixed(1)}%',
                  //   style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  //     color: colorScheme.onSurfaceVariant,
                  //   ),
                  // ),
                ],
              ),
            ] else if (task.status == UploadStatus.UPLOADING ||
                task.status == UploadStatus.PAUSED) ...[
              Row(
                children: [
                  if (task.speed > 0) ...[
                    const SizedBox(width: 8),
                    Text(
                      task.formatSpeed,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                  const Spacer(),
                  Text(
                    '${task.progress.toStringAsFixed(1)}%',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
              // 显示已上传大小
              if (task.uploadedBytes > 0)
                Padding(
                  padding: const EdgeInsets.only(top: 4),
                  child: Row(
                    children: [
                      Text(
                        al.uploadTask_uploaded(_formatBytes(task.uploadedBytes)),
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: colorScheme.onSurfaceVariant,
                        ),
                      ),
                      const Spacer(),
                      Text(
                        al.uploadTask_total(_formatBytes(task.fileSize)),
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                ),
            ],
            // 错误信息
            if (task.status == UploadStatus.FAILED && task.errorMessage != null)
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

  /// 格式化字节大小
  String _formatBytes(int bytes) {
    if (bytes < 1024) {
      return '$bytes B';
    } else if (bytes < 1024 * 1024) {
      return '${(bytes / 1024).toStringAsFixed(1)} KB';
    } else if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(2)} MB';
    } else {
      return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
    }
  }

  IconData _getFileIcon() {
    // 根据文件名后缀返回图标
    final ext = task.filename.split('.').last.toLowerCase();
    switch (ext) {
      case 'jpg':
      case 'jpeg':
      case 'png':
      case 'gif':
      case 'webp':
        return Icons.image;
      case 'mp4':
      case 'avi':
      case 'mkv':
      case 'mov':
        return Icons.video_file;
      case 'mp3':
      case 'wav':
      case 'flac':
        return Icons.audio_file;
      case 'pdf':
        return Icons.picture_as_pdf;
      case 'doc':
      case 'docx':
        return Icons.description;
      case 'xls':
      case 'xlsx':
        return Icons.table_chart;
      case 'zip':
      case 'rar':
      case '7z':
        return Icons.folder_zip;
      default:
        return Icons.insert_drive_file;
    }
  }

  Widget _buildStatusChip(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    String label;
    Color color;

    switch (task.status) {
      case UploadStatus.PENDING:
        label = al.uploadTask_statusWaiting;
        color = Colors.grey;
        break;
      case UploadStatus.UPLOADING:
        label = al.uploadTask_statusUploading;
        color = Theme.of(context).colorScheme.primary;
        break;
      case UploadStatus.COMPLETED:
        label = al.uploadTask_statusCompleted;
        color = Colors.green;
        break;
      case UploadStatus.PAUSED:
        label = al.uploadTask_statusPaused;
        color = Colors.orange;
        break;
      case UploadStatus.CANCELED:
        label = al.uploadTask_statusCanceled;
        color = Colors.grey;
        break;
      case UploadStatus.FAILED:
        label = al.uploadTask_statusFailed;
        color = Theme.of(context).colorScheme.error;
        break;
      case UploadStatus.HASHING:
        label = al.uploadTask_statusHashing;
        color = Colors.grey;
        break;
      default:
        label = al.common_unknown;
        color = Colors.grey;
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

  Widget _buildActions(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    return PopupMenuButton<String>(
      icon: const Icon(Icons.more_vert, size: 20),
      onSelected: (value) {
        final bloc = context.read<TransferBloc>();
        switch (value) {
          case 'pause':
            bloc.add(TransferPauseEvent(task.id));
            break;
          case 'resume':
            bloc.add(TransferResumeEvent(task.id));
            break;
          case 'cancel':
            bloc.add(TransferCancelEvent(task.id));
            break;
          case 'retry':
            bloc.add(TransferResumeEvent(task.id));
            break;
          case 'rename_retry':
            _showRenameDialog(context, bloc);
            break;
          case 'remove':
            bloc.add(TransferRemoveEvent(task.id));
            break;
          case 'detail':
            _showDetailSheet(context);
            break;
        }
      },
      itemBuilder: (context) {
        final detailItem = PopupMenuItem(
          value: 'detail',
          child: ListTile(
            leading: Icon(Icons.info_outline),
            title: Text(AppLocalizations.of(context).uploadRecord_viewDetail),
            dense: true,
          ),
        );
        if (task.isActive) {
          return [
            PopupMenuItem(value: 'pause', child: Text(al.uploadTask_pause)),
            PopupMenuItem(value: 'cancel', child: Text(al.uploadTask_cancel)),
            detailItem,
            PopupMenuItem(value: 'remove', child: Text(al.common_remove)),
          ];
        } else if (task.status == UploadStatus.PAUSED) {
          return [
            PopupMenuItem(value: 'resume', child: Text(al.uploadTask_resume)),
            PopupMenuItem(value: 'cancel', child: Text(al.uploadTask_cancel)),
            detailItem,
            PopupMenuItem(value: 'remove', child: Text(al.common_remove)),
          ];
        } else if (task.status == UploadStatus.FAILED) {
          return [
            PopupMenuItem(value: 'retry', child: Text(al.uploadTask_retry)),
            PopupMenuItem(value: 'rename_retry', child: Text('重命名重试')),
            PopupMenuItem(value: 'cancel', child: Text(al.uploadTask_cancel)),
            detailItem,
            PopupMenuItem(value: 'remove', child: Text(al.common_remove)),
          ];
        } else {
          return [
            detailItem,
            PopupMenuItem(value: 'remove', child: Text(al.common_remove)),
          ];
        }
      },
    );
  }

  /// 显示重命名对话框
  void _showRenameDialog(BuildContext context, TransferBloc bloc) {
    final al = AppLocalizations.of(context)!;
    final controller = TextEditingController(text: task.filename);
    
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('重命名并重试'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text('输入新的文件名：'),
            const SizedBox(height: 16),
            TextField(
              controller: controller,
              decoration: InputDecoration(
                labelText: '文件名',
                border: OutlineInputBorder(),
              ),
              autofocus: true,
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () {
              final newFilename = controller.text.trim();
              if (newFilename.isNotEmpty && newFilename != task.filename) {
                bloc.add(TransferRenameAndRetryEvent(task.id, newFilename));
                Navigator.of(ctx).pop();
              }
            },
            child: Text('重试'),
          ),
        ],
      ),
    );
  }

  void _showDetailSheet(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    final dateFormat = DateFormat('yyyy-MM-dd HH:mm:ss');

    String statusText;
    switch (task.status) {
      case UploadStatus.PENDING:
        statusText = al.uploadTask_statusWaiting;
        break;
      case UploadStatus.UPLOADING:
        statusText = al.uploadTask_statusUploading;
        break;
      case UploadStatus.COMPLETED:
        statusText = al.uploadTask_statusCompleted;
        break;
      case UploadStatus.PAUSED:
        statusText = al.uploadTask_statusPaused;
        break;
      case UploadStatus.CANCELED:
        statusText = al.uploadTask_statusCanceled;
        break;
      case UploadStatus.FAILED:
        statusText = al.uploadTask_statusFailed;
        break;
      case UploadStatus.HASHING:
        statusText = al.uploadTask_statusHashing;
        break;
      default:
        statusText = al.common_unknown;
    }

    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      builder: (ctx) => DraggableScrollableSheet(
        initialChildSize: 0.6,
        minChildSize: 0.3,
        maxChildSize: 0.9,
        expand: false,
        builder: (ctx, scrollController) => SingleChildScrollView(
          controller: scrollController,
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // 拖拽手柄
              Center(
                child: Container(
                  width: 40,
                  height: 4,
                  margin: const EdgeInsets.only(bottom: 16),
                  decoration: BoxDecoration(
                    color: Theme.of(ctx).colorScheme.onSurfaceVariant.withValues(alpha: 0.4),
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
              ),
              // 文件名
              Text(
                task.filename,
                style: Theme.of(ctx).textTheme.titleLarge,
              ),
              const SizedBox(height: 16),
              // 详情列表
              _detailRow(ctx, '状态', statusText),
              _detailRow(ctx, '文件大小', formatCapacityString(task.fileSize)),
              _detailRow(ctx, '上传进度', '${task.progress.toStringAsFixed(1)}%'),
              if (task.uploadedBytes > 0)
                _detailRow(ctx, '已上传', _formatBytes(task.uploadedBytes)),
              if (task.speed > 0)
                _detailRow(ctx, '上传速度', task.formatSpeed),
              _detailRow(ctx, '本地路径', task.filepath),
              _detailRow(ctx, '目标目录 ID', task.parentId),
              if (task.fileHash != null && task.fileHash!.isNotEmpty)
                _detailRow(ctx, '文件哈希 (Blake3)', task.fileHash!),
              if (task.chunkTotal > 0)
                _detailRow(ctx, '分片信息', '${task.chunkTotal} 片 × ${formatCapacityString(task.chunkSize)}'),
              if (task.tags != null && task.tags!.isNotEmpty)
                _detailRow(ctx, '标签', task.tags!),
              _detailRow(ctx, '覆盖同名', task.overwrite ? '是' : '否'),
              _detailRow(ctx, '重试次数', '${task.retryCount}'),
              _detailRow(ctx, '创建时间', dateFormat.format(task.createdAt)),
              if (task.completedAt != null)
                _detailRow(ctx, '完成时间', dateFormat.format(task.completedAt!)),
              if (task.errorMessage != null) ...[
                const SizedBox(height: 8),
                Text('错误信息', style: Theme.of(ctx).textTheme.labelMedium),
                const SizedBox(height: 4),
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: Theme.of(ctx).colorScheme.errorContainer,
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: Text(
                    task.errorMessage!,
                    style: TextStyle(color: Theme.of(ctx).colorScheme.onErrorContainer),
                  ),
                ),
              ],
              const SizedBox(height: 24),
              // 打开本地文件所在目录
              SizedBox(
                width: double.infinity,
                child: FilledButton.icon(
                  onPressed: () => _openLocalDirectory(ctx),
                  icon: const Icon(Icons.folder_open),
                  label: const Text('打开本地目录'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _detailRow(BuildContext context, String label, String value) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 100,
            child: Text(
              label,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _openLocalDirectory(BuildContext context) async {
    final dir = File(task.filepath).parent.path;
    try {
      if (isDesktop()) {
        if (Platform.isWindows) {
          await Process.run('explorer', [dir]);
        } else if (Platform.isMacOS) {
          await Process.run('open', [dir]);
        } else if (Platform.isLinux) {
          await Process.run('xdg-open', [dir]);
        }
      } else {
        // 移动端无法直接打开目录，复制路径到剪贴板
        await Clipboard.setData(ClipboardData(text: task.filepath));
        if (context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('已复制路径到剪贴板: ${task.filepath}')),
          );
        }
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('打开失败: $e')),
        );
      }
    }
  }
}
