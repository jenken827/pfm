import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/widgets/download_task_list.dart';
import 'package:pfm/widgets/upload_task_list.dart';
import 'package:pfm/widgets/upload_record_list.dart';

/// 传输页面 - 包含上传和下载标签页
class TransferPage extends StatefulWidget {
  const TransferPage({super.key});

  @override
  State<TransferPage> createState() => _TransferPageState();
}

class _TransferPageState extends State<TransferPage>
    with SingleTickerProviderStateMixin {
  late TabController _tabController;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 3, vsync: this);
    _tabController.addListener(_onTabChanged);
  }

  @override
  void dispose() {
    _tabController.removeListener(_onTabChanged);
    _tabController.dispose();
    super.dispose();
  }

  void _onTabChanged() {
    if (_tabController.indexIsChanging) return;
    TransferTab tab;
    switch (_tabController.index) {
      case 0:
        tab = TransferTab.upload;
        break;
      case 1:
        tab = TransferTab.download;
        break;
      case 2:
        tab = TransferTab.uploadRecord;
        break;
      default:
        tab = TransferTab.upload;
    }
    context.read<TransferBloc>().add(TransferTabChangedEvent(tab));
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;

    return BlocProvider.value(
      value: GetIt.I.get<TransferBloc>(),
      child: Builder(
        builder: (context) => Scaffold(
          appBar: AppBar(
            title: Text(al.transfer),
            centerTitle: true,
            bottom: TabBar(
              controller: _tabController,
              tabs: [
                Tab(
                  icon: const Icon(Icons.cloud_upload_outlined),
                  text: '上传', // al.upload,
                ),
                Tab(
                  icon: const Icon(Icons.cloud_download_outlined),
                  text: '下载', // al.download,
                ),
                Tab(
                  icon: const Icon(Icons.history),
                  text: '上传记录',
                ),
              ],
            ),
            actions: [
              // 刷新按钮
              IconButton(
                icon: const Icon(Icons.refresh),
                onPressed: () {
                  context.read<TransferBloc>().add(TransferRefreshEvent());
                },
                tooltip: '刷新',
              ),
            ],
          ),
          body: BlocBuilder<TransferBloc, TransferState>(
            // buildWhen: (previous, current) =>
            //     previous.currentTab != current.currentTab,
            builder: (context, state) {
              return TabBarView(
                controller: _tabController,
                children: [
                  // 上传任务列表
                  UploadTaskList(),
                  // 下载任务列表
                  DownloadTaskList(),
                  // 上传记录列表
                  UploadRecordList(),
                ],
              );
            },
          ),
        ),
      ),
    );
  }
}
