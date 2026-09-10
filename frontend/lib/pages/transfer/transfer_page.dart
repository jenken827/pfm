import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/pages/transfer/offline_download_page.dart';
import 'package:pfm/widgets/download_task_list.dart';
import 'package:pfm/widgets/upload_task_list.dart';
import 'package:pfm/widgets/upload_record_list.dart';
import 'package:pfm/design/design_tokens.dart';

/// 传输页面 - Clean Minimal 设计
class TransferPage extends StatefulWidget {
  const TransferPage({super.key});

  @override
  State<TransferPage> createState() => _TransferPageState();
}

class _TransferPageState extends State<TransferPage>
    with SingleTickerProviderStateMixin {
  late TabController _tabController;
  VoidCallback? _uploadRecordRefreshCallback;

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
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;
    final screenWidth = MediaQuery.of(context).size.width;
    final isMobile = screenWidth < PfmConstraints.mobileBreakpoint;

    return BlocProvider.value(
      value: GetIt.I.get<TransferBloc>(),
      child: Builder(
        builder: (context) => Scaffold(
          body: SafeArea(
            child: Column(
              children: [
                // 工具栏
                _buildToolbar(context, al, colorScheme, isMobile),
                // Tab 栏
                _buildTabBar(context, al, colorScheme),
                // 内容
                Expanded(
                  child: BlocBuilder<TransferBloc, TransferState>(
                    buildWhen: (previous, current) =>
                        previous.currentTab != current.currentTab,
                    builder: (context, state) {
                      return TabBarView(
                        controller: _tabController,
                        children: [
                          UploadTaskList(),
                          DownloadTaskList(),
                          UploadRecordList(
                            onRefreshRegistered: (callback) {
                              _uploadRecordRefreshCallback = callback;
                            },
                          ),
                        ],
                      );
                    },
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildToolbar(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
    bool isMobile,
  ) {
    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: isMobile ? PfmSpacing.lg : PfmSpacing.xxxxl,
        vertical: PfmSpacing.lg,
      ),
      decoration: BoxDecoration(
        color: colorScheme.surface,
        border: Border(bottom: BorderSide(color: colorScheme.outlineVariant)),
      ),
      child: Row(
        children: [
          Text(
            al.nav_transfer,
            style: PfmTypography.titleLarge.copyWith(
              color: colorScheme.onSurface,
            ),
          ),
          const Spacer(),
          // 离线下载任务管理（服务器 aria2 任务）
          IconButton(
            icon: Icon(
              Icons.cloud_download_outlined,
              size: 20,
              color: colorScheme.onSurfaceVariant,
            ),
            tooltip: al.offlineDl_title,
            visualDensity: VisualDensity.compact,
            onPressed: () => Navigator.push(
              context,
              MaterialPageRoute(
                builder: (context) => const OfflineDownloadPage(),
              ),
            ),
          ),
          const SizedBox(width: PfmSpacing.xs),
          IconButton(
            icon: Icon(
              Icons.refresh,
              size: 20,
              color: colorScheme.onSurfaceVariant,
            ),
            onPressed: () {
              if (_tabController.index == 2) {
                _uploadRecordRefreshCallback?.call();
              } else {
                context.read<TransferBloc>().add(TransferRefreshEvent());
              }
            },
            tooltip: al.transfer_refresh,
            visualDensity: VisualDensity.compact,
          ),
        ],
      ),
    );
  }

  Widget _buildTabBar(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
  ) {
    return Container(
      decoration: BoxDecoration(
        color: colorScheme.surface,
        border: Border(bottom: BorderSide(color: colorScheme.outlineVariant)),
      ),
      child: TabBar(
        controller: _tabController,
        labelColor: colorScheme.primary,
        unselectedLabelColor: colorScheme.onSurfaceVariant,
        labelStyle: PfmTypography.labelLarge,
        unselectedLabelStyle: PfmTypography.labelLarge,
        indicatorColor: colorScheme.primary,
        indicatorWeight: 2,
        indicatorSize: TabBarIndicatorSize.label,
        dividerColor: Colors.transparent,
        tabs: [
          Tab(
            icon: const Icon(Icons.cloud_upload_outlined, size: 18),
            text: al.transfer_upload,
          ),
          Tab(
            icon: const Icon(Icons.cloud_download_outlined, size: 18),
            text: al.transfer_download,
          ),
          Tab(
            icon: const Icon(Icons.history, size: 18),
            text: al.transfer_cloudUploadRecords,
          ),
        ],
      ),
    );
  }
}
