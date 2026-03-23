// import 'package:flutter/material.dart';
// import 'package:flutter_bloc/flutter_bloc.dart';
// import 'package:get_it/get_it.dart';
// import 'package:pfm/blocs/upload_bloc/upload_bloc.dart';
// import 'package:pfm/blocs/upload_bloc/upload_event.dart';
// import 'package:pfm/blocs/upload_bloc/upload_state.dart';
// import 'package:pfm/utils/format.dart';

// /// 上传进度面板 - 显示在页面底部
// class UploadProgressPanel extends StatelessWidget {
//   const UploadProgressPanel({super.key});

//   @override
//   Widget build(BuildContext context) {
//     return BlocBuilder<UploadBloc, UploadState>(
//       bloc: GetIt.I.get<UploadBloc>(),
//       builder: (context, state) {
//         // 没有上传任务时不显示
//         if (state.tasks.isEmpty) {
//           return const SizedBox.shrink();
//         }

//         final uploadingTasks = state.uploadingTasks;
//         final completedTasks = state.completedTasks;
//         final failedTasks = state.failedTasks;

//         return Container(
//           decoration: BoxDecoration(
//             color: Theme.of(context).colorScheme.surfaceContainerHigh,
//             boxShadow: [
//               BoxShadow(
//                 color: Colors.black.withValues(alpha: 0.1),
//                 blurRadius: 8,
//                 offset: const Offset(0, -2),
//               ),
//             ],
//           ),
//           child: SafeArea(
//             top: false,
//             child: Column(
//               mainAxisSize: MainAxisSize.min,
//               children: [
//                 // 标题栏
//                 _buildHeader(context, state),
//                 // 任务列表
//                 if (uploadingTasks.isNotEmpty || failedTasks.isNotEmpty)
//                   ConstrainedBox(
//                     constraints: const BoxConstraints(maxHeight: 200),
//                     child: ListView.builder(
//                       shrinkWrap: true,
//                       padding: const EdgeInsets.symmetric(horizontal: 8),
//                       itemCount: uploadingTasks.length + failedTasks.length,
//                       itemBuilder: (context, index) {
//                         if (index < uploadingTasks.length) {
//                           return _UploadTaskItem(
//                             task: uploadingTasks[index],
//                             showActions: true,
//                           );
//                         }
//                         return _UploadTaskItem(
//                           task: failedTasks[index - uploadingTasks.length],
//                           showActions: true,
//                         );
//                       },
//                     ),
//                   ),
//                 // 已完成提示
//                 if (completedTasks.isNotEmpty)
//                   Padding(
//                     padding: const EdgeInsets.all(8),
//                     child: Row(
//                       children: [
//                         Icon(
//                           Icons.check_circle,
//                           color: Theme.of(context).colorScheme.primary,
//                           size: 16,
//                         ),
//                         const SizedBox(width: 8),
//                         Text(
//                           '${completedTasks.length} 个文件上传完成',
//                           style: Theme.of(context).textTheme.bodySmall,
//                         ),
//                         const Spacer(),
//                         TextButton(
//                           onPressed: () => GetIt.I.get<UploadBloc>().add(
//                             const UploadClearCompletedEvent(),
//                           ),
//                           child: const Text('清除'),
//                         ),
//                       ],
//                     ),
//                   ),
//               ],
//             ),
//           ),
//         );
//       },
//     );
//   }

//   Widget _buildHeader(BuildContext context, UploadState state) {
//     final uploadingCount = state.uploadingTasks.length;
//     final totalCount = state.tasks.length;

//     return Padding(
//       padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
//       child: Row(
//         children: [
//           Icon(
//             Icons.cloud_upload,
//             color: Theme.of(context).colorScheme.primary,
//           ),
//           const SizedBox(width: 8),
//           Text(
//             uploadingCount > 0
//                 ? '正在上传 $uploadingCount 个文件'
//                 : '上传队列 ($totalCount)',
//             style: Theme.of(context).textTheme.titleSmall,
//           ),
//           const Spacer(),
//           // 整体进度
//           if (uploadingCount > 0) ...[
//             Text(
//               '${state.totalProgress.toStringAsFixed(1)}%',
//               style: Theme.of(context).textTheme.bodySmall,
//             ),
//             const SizedBox(width: 8),
//           ],
//         ],
//       ),
//     );
//   }
// }

// /// 单个上传任务项
// class _UploadTaskItem extends StatelessWidget {
//   const _UploadTaskItem({required this.task, this.showActions = false});

//   final UploadTaskState task;
//   final bool showActions;

//   @override
//   Widget build(BuildContext context) {
//     final colorScheme = Theme.of(context).colorScheme;

//     return Card(
//       margin: const EdgeInsets.symmetric(vertical: 4),
//       child: Padding(
//         padding: const EdgeInsets.all(12),
//         child: Column(
//           crossAxisAlignment: CrossAxisAlignment.start,
//           children: [
//             // 文件名和状态
//             Row(
//               children: [
//                 Expanded(
//                   child: Text(
//                     task.fileName,
//                     maxLines: 1,
//                     overflow: TextOverflow.ellipsis,
//                     style: Theme.of(context).textTheme.bodyMedium,
//                   ),
//                 ),
//                 const SizedBox(width: 8),
//                 _buildStatusChip(context),
//                 if (showActions) _buildActions(context),
//               ],
//             ),
//             const SizedBox(height: 8),
//             // 进度条
//             if (task.isUploading) ...[
//               LinearProgressIndicator(
//                 value: task.progress / 100,
//                 backgroundColor: colorScheme.surfaceContainerHighest,
//                 borderRadius: BorderRadius.circular(4),
//                 minHeight: 6,
//               ),
//               const SizedBox(height: 4),
//               // 进度信息
//               Row(
//                 children: [
//                   Text(
//                     '${task.uploadedChunks}/${task.totalChunks} 分片',
//                     style: Theme.of(context).textTheme.bodySmall?.copyWith(
//                       color: colorScheme.onSurfaceVariant,
//                     ),
//                   ),
//                   const Spacer(),
//                   Text(
//                     '${formatCapacityString(task.uploadedSize)} / ${formatCapacityString(task.fileSize)}',
//                     style: Theme.of(context).textTheme.bodySmall?.copyWith(
//                       color: colorScheme.onSurfaceVariant,
//                     ),
//                   ),
//                 ],
//               ),
//             ],
//             // 错误信息
//             if (task.isFailed && task.errorMessage != null)
//               Padding(
//                 padding: const EdgeInsets.only(top: 4),
//                 child: Text(
//                   task.errorMessage!,
//                   style: Theme.of(
//                     context,
//                   ).textTheme.bodySmall?.copyWith(color: colorScheme.error),
//                   maxLines: 2,
//                   overflow: TextOverflow.ellipsis,
//                 ),
//               ),
//           ],
//         ),
//       ),
//     );
//   }

//   Widget _buildStatusChip(BuildContext context) {
//     String label;
//     Color color;

//     switch (task.status) {
//       case UploadStatus.UPLOAD_STATUS_PENDING:
//         label = '等待中';
//         color = Colors.grey;
//         break;
//       case UploadTaskStatus.uploading:
//         label = '上传中';
//         color = Theme.of(context).colorScheme.primary;
//         break;
//       case UploadTaskStatus.completing:
//         label = '完成中';
//         color = Theme.of(context).colorScheme.tertiary;
//         break;
//       case UploadTaskStatus.completed:
//         label = '已完成';
//         color = Colors.green;
//         break;
//       case UploadTaskStatus.paused:
//         label = '已暂停';
//         color = Colors.orange;
//         break;
//       case UploadTaskStatus.cancelled:
//         label = '已取消';
//         color = Colors.grey;
//         break;
//       case UploadTaskStatus.failed:
//         label = '失败';
//         color = Theme.of(context).colorScheme.error;
//         break;
//       case UploadTaskStatus.initializing:
//         label = '初始化';
//         color = Colors.blue;
//         break;
//     }

//     return Container(
//       padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
//       decoration: BoxDecoration(
//         color: color.withValues(alpha: 0.1),
//         borderRadius: BorderRadius.circular(10),
//         border: Border.all(color: color.withValues(alpha: 0.3)),
//       ),
//       child: Text(
//         label,
//         style: TextStyle(
//           fontSize: 11,
//           color: color,
//           fontWeight: FontWeight.w500,
//         ),
//       ),
//     );
//   }

//   Widget _buildActions(BuildContext context) {
//     return PopupMenuButton<String>(
//       icon: const Icon(Icons.more_vert, size: 20),
//       onSelected: (value) {
//         final bloc = GetIt.I.get<UploadBloc>();
//         switch (value) {
//           case 'pause':
//             bloc.add(UploadPauseEvent(task.taskId));
//             break;
//           case 'resume':
//             bloc.add(UploadResumeEvent(task.taskId));
//             break;
//           case 'cancel':
//             bloc.add(UploadCancelEvent(task.taskId));
//             break;
//           case 'retry':
//             bloc.add(UploadRetryEvent(task.taskId));
//             break;
//           case 'remove':
//             bloc.add(UploadRemoveEvent(task.taskId));
//             break;
//         }
//       },
//       itemBuilder: (context) => [
//         if (task.isUploading)
//           const PopupMenuItem(value: 'pause', child: Text('暂停')),
//         if (task.isPaused)
//           const PopupMenuItem(value: 'resume', child: Text('继续')),
//         if (task.isFailed)
//           const PopupMenuItem(value: 'retry', child: Text('重试')),
//         if (task.isUploading || task.isPaused)
//           const PopupMenuItem(value: 'cancel', child: Text('取消')),
//         const PopupMenuItem(value: 'remove', child: Text('移除')),
//       ],
//     );
//   }
// }
