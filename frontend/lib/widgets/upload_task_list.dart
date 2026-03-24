import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/utils/format.dart';

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
            '暂无上传任务',
            style: Theme.of(context).textTheme.titleMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            '在文件页面选择文件开始上传',
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildActionBar(BuildContext context, TransferState state) {
    final uploadingCount = state.uploadingCount;
    final completedCount = state.completedUploadCount;
    final failedCount = state.failedUploadCount;

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
          // 状态统计
          if (uploadingCount > 0)
            _buildStatusChip(
              context,
              '上传中 $uploadingCount',
              Theme.of(context).colorScheme.primary,
            ),
          if (completedCount > 0)
            _buildStatusChip(context, '已完成 $completedCount', Colors.green),
          if (failedCount > 0)
            _buildStatusChip(
              context,
              '失败 $failedCount',
              Theme.of(context).colorScheme.error,
            ),
          const Spacer(),
          // 操作按钮
          if (uploadingCount > 0) ...[
            TextButton.icon(
              onPressed: () =>
                  context.read<TransferBloc>().add(TransferPauseEvent(null)),
              icon: const Icon(Icons.pause, size: 18),
              label: const Text('全部暂停'),
            ),
            const SizedBox(width: 8),
          ],
          if (completedCount > 0 || failedCount > 0)
            TextButton.icon(
              onPressed: () => context.read<TransferBloc>().add(
                TransferClearCompletedEvent(),
              ),
              icon: const Icon(Icons.clear_all, size: 18),
              label: const Text('清除已完成'),
            ),
        ],
      ),
    );
  }

  Widget _buildStatusChip(BuildContext context, String label, Color color) {
    return Container(
      margin: const EdgeInsets.only(right: 8),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 12,
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
              value: task.progress,
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
                    '正在计算文件指纹...',
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
                        '已上传: ${_formatBytes(task.uploadedBytes)}',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: colorScheme.onSurfaceVariant,
                        ),
                      ),
                      const Spacer(),
                      Text(
                        '总计: ${_formatBytes(task.fileSize)}',
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
    String label;
    Color color;

    switch (task.status) {
      case UploadStatus.PENDING:
        label = '等待中';
        color = Colors.grey;
        break;
      case UploadStatus.UPLOADING:
        label = '上传中';
        color = Theme.of(context).colorScheme.primary;
        break;
      case UploadStatus.COMPLETED:
        label = '已完成';
        color = Colors.green;
        break;
      case UploadStatus.PAUSED:
        label = '已暂停';
        color = Colors.orange;
        break;
      case UploadStatus.CANCELED:
        label = '已取消';
        color = Colors.grey;
        break;
      case UploadStatus.FAILED:
        label = '失败';
        color = Theme.of(context).colorScheme.error;
        break;
      case UploadStatus.HASHING:
        label = '计算指纹中';
        color = Colors.grey;
        break;
      default:
        label = '未知';
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
          case 'remove':
            bloc.add(TransferRemoveEvent(task.id));
            break;
        }
      },
      itemBuilder: (context) {
        if (task.isActive) {
          return [
            const PopupMenuItem(value: 'pause', child: Text('暂停')),
            const PopupMenuItem(value: 'cancel', child: Text('取消')),
            const PopupMenuItem(value: 'remove', child: Text('移除')),
          ];
        } else if (task.status == UploadStatus.PAUSED) {
          return [
            const PopupMenuItem(value: 'resume', child: Text('继续')),
            const PopupMenuItem(value: 'cancel', child: Text('取消')),
            const PopupMenuItem(value: 'remove', child: Text('移除')),
          ];
        } else if (task.status == UploadStatus.FAILED) {
          return [
            const PopupMenuItem(value: 'retry', child: Text('重试')),
            const PopupMenuItem(value: 'cancel', child: Text('取消')),
            const PopupMenuItem(value: 'remove', child: Text('移除')),
          ];
        } else {
          return [const PopupMenuItem(value: 'remove', child: Text('移除'))];
        }
      },
    );
  }
}
