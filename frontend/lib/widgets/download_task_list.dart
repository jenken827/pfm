import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/blocs/blocs.dart';
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
            '暂无下载任务',
            style: Theme.of(context).textTheme.titleMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            '点击文件开始下载',
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
                  onSelected: (value) {
                    // TODO: 实现下载操作
                  },
                  itemBuilder: (context) => [
                    if (task.isDownloading)
                      const PopupMenuItem(value: 'pause', child: Text('暂停')),
                    if (task.isPaused)
                      const PopupMenuItem(value: 'resume', child: Text('继续')),
                    if (task.isFailed)
                      const PopupMenuItem(value: 'retry', child: Text('重试')),
                    if (task.isDownloading || task.isPaused)
                      const PopupMenuItem(value: 'cancel', child: Text('取消')),
                    const PopupMenuItem(value: 'remove', child: Text('移除')),
                  ],
                ),
              ],
            ),
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

  Widget _buildStatusChip(BuildContext context) {
    String label;
    Color color;

    switch (task.status) {
      case DownloadTaskStatus.pending:
        label = '等待中';
        color = Colors.grey;
        break;
      case DownloadTaskStatus.downloading:
        label = '下载中';
        color = Theme.of(context).colorScheme.primary;
        break;
      case DownloadTaskStatus.completed:
        label = '已完成';
        color = Colors.green;
        break;
      case DownloadTaskStatus.paused:
        label = '已暂停';
        color = Colors.orange;
        break;
      case DownloadTaskStatus.cancelled:
        label = '已取消';
        color = Colors.grey;
        break;
      case DownloadTaskStatus.failed:
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
}
