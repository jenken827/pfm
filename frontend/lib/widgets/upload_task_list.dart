import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/blocs/transfer_bloc/transfer_bloc.dart';
import 'package:pfm/blocs/transfer_bloc/transfer_event.dart';
import 'package:pfm/blocs/transfer_bloc/transfer_state.dart';
import 'package:pfm/blocs/upload_bloc/upload_bloc.dart';
import 'package:pfm/blocs/upload_bloc/upload_event.dart';
import 'package:pfm/blocs/upload_bloc/upload_state.dart';
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
              onPressed: () => context.read<TransferBloc>().add(
                TransferPauseAllUploadsEvent(),
              ),
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

  final UploadTaskState task;

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
                // 状态
                _buildStatusChip(context),
                // 操作菜单
                _buildActions(context),
              ],
            ),
            // 进度条
            if (task.isHashing) ...[
              // 哈希计算进度
              const SizedBox(height: 12),
              LinearProgressIndicator(
                value: task.hashProgress / 100,
                backgroundColor: colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(4),
                minHeight: 6,
              ),
              const SizedBox(height: 8),
              Row(
                children: [
                  Text(
                    '正在计算文件指纹...',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const Spacer(),
                  Text(
                    '${task.hashProgress.toStringAsFixed(1)}%',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ] else if (task.isUploading || task.isPaused) ...[
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
                    '${task.uploadedChunks}/${task.totalChunks} 分片',
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
            // 错误信息
            if (task.isFailed && task.errorMessage != null)
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

  IconData _getFileIcon() {
    // 根据文件名后缀返回图标
    final ext = task.fileName.split('.').last.toLowerCase();
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
      case UploadTaskStatus.pending:
        if (task.isHashing) {
          label = '计算中';
          color = Colors.blue;
        } else {
          label = '等待中';
          color = Colors.grey;
        }
        break;
      case UploadTaskStatus.initializing:
        label = '初始化';
        color = Colors.blue;
        break;
      case UploadTaskStatus.uploading:
        label = '上传中';
        color = Theme.of(context).colorScheme.primary;
        break;
      case UploadTaskStatus.completing:
        label = '完成中';
        color = Theme.of(context).colorScheme.tertiary;
        break;
      case UploadTaskStatus.completed:
        label = '已完成';
        color = Colors.green;
        break;
      case UploadTaskStatus.paused:
        label = '已暂停';
        color = Colors.orange;
        break;
      case UploadTaskStatus.cancelled:
        label = '已取消';
        color = Colors.grey;
        break;
      case UploadTaskStatus.failed:
        label = '失败';
        color = Theme.of(context).colorScheme.error;
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
        final uploadBloc = GetIt.I.get<UploadBloc>();
        switch (value) {
          case 'pause':
            uploadBloc.add(UploadPauseEvent(task.taskId));
            break;
          case 'resume':
            uploadBloc.add(UploadResumeEvent(task.taskId));
            break;
          case 'cancel':
            uploadBloc.add(UploadCancelEvent(task.taskId));
            break;
          case 'retry':
            uploadBloc.add(UploadRetryEvent(task.taskId));
            break;
          case 'remove':
            uploadBloc.add(UploadRemoveEvent(task.taskId));
            break;
        }
      },
      itemBuilder: (context) => [
        if (task.isUploading)
          const PopupMenuItem(value: 'pause', child: Text('暂停')),
        if (task.isPaused)
          const PopupMenuItem(value: 'resume', child: Text('继续')),
        if (task.isFailed)
          const PopupMenuItem(value: 'retry', child: Text('重试')),
        if (task.isUploading || task.isPaused)
          const PopupMenuItem(value: 'cancel', child: Text('取消')),
        const PopupMenuItem(value: 'remove', child: Text('移除')),
      ],
    );
  }
}
