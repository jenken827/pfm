import 'package:flutter/material.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/services/call_recording_backup_service.dart';
import 'package:pfm/services/device_backup_content.dart';
import 'package:pfm/services/device_backup_service.dart';
import 'package:pfm/utils/platform.dart';

/// 设备备份页的页签
///
/// 短信与通话记录共用 [DeviceBackupService]（同一套"导出文本"逻辑），
/// 录音则是另一套服务（[CallRecordingBackupService]，搬运磁盘文件），
/// 故这里单独定义页签枚举，而不是复用 DeviceBackupType。
enum DeviceBackupTab { sms, callLog, callRecording }

/// 短信、通话记录与通话录音备份页面（任务9）
///
/// 移动端（Android）可用：
/// - 短信/通话记录：读取系统数据 → 导出文本 → 后台上传
/// - 通话录音：扫描 ROM 录音目录 → 增量比对 → 后台上传
/// 桌面端仅提示。
class DeviceBackupPage extends StatefulWidget {
  const DeviceBackupPage({super.key});

  @override
  State<DeviceBackupPage> createState() => _DeviceBackupPageState();
}

class _DeviceBackupPageState extends State<DeviceBackupPage>
    with SingleTickerProviderStateMixin {
  late DeviceBackupService _service;
  bool _isLoading = true;

  /// Tab 控制器。
  ///
  /// 必须显式提供：本页内容不是 TabBarView，而是由 [_tab] 驱动的单个列表，
  /// 页面外也没有 DefaultTabController —— 此时 TabBar 找不到控制器会抛
  /// "No TabController for TabBar"。
  late final TabController _tabController;

  DeviceBackupTab _tab = DeviceBackupTab.sms;

  /// 各 Tab 结果快照（用 setState 刷新）
  DeviceBackupResult _smsResult = const DeviceBackupResult();
  DeviceBackupResult _callResult = const DeviceBackupResult();

  /// 录音备份服务与状态
  late CallRecordingBackupService _recordingService;
  CallRecordingBackupResult _recordingResult =
      const CallRecordingBackupResult();

  /// 录音目录的 SAF 授权是否有效（授权可能被撤销/目录被删，
  /// 每次进页面都重新确认，不做乐观假设）
  bool _hasDirAccess = false;

  /// 本机设备标识（短信/通话记录按设备独有，展示出来避免多设备备份混淆）
  String _deviceInfo = '';

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 3, vsync: this);
    // 从控制器同步 _tab，而不是用 TabBar.onTap：onTap 只在点击时触发，
    // 且触发时机在控制器动画之后；用 listener 可保证指示器与内容一致，
    // 将来若接入滑动切换也无需再改。
    _tabController.addListener(() {
      // 动画过程中 index 会变，只在稳定后同步，避免重复 setState
      if (_tabController.indexIsChanging) return;
      final next = DeviceBackupTab.values[_tabController.index];
      if (next != _tab) setState(() => _tab = next);
    });
    _initService();
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
  }

  Future<void> _initService() async {
    _service = await DeviceBackupService.instance();
    _smsResult = _service.result(DeviceBackupType.sms);
    _callResult = _service.result(DeviceBackupType.callLog);

    _service
        .statusStream(DeviceBackupType.sms)
        .listen((r) => setState(() => _smsResult = r));
    _service
        .statusStream(DeviceBackupType.callLog)
        .listen((r) => setState(() => _callResult = r));

    // 设备标识在导出时用于文件名与内容，这里先取出来给用户看
    final deviceInfo = await _service.deviceInfo();

    // 录音备份是独立服务（搬运磁盘文件，与短信/通话记录的"导出"逻辑不同）
    _recordingService = await CallRecordingBackupService.instance();
    _recordingResult = _recordingService.result;
    _recordingService.statusStream.listen((r) {
      if (mounted) setState(() => _recordingResult = r);
    });

    // 不再自动探测目录：录音目录需用户经 SAF 显式授权（各 ROM 路径不同，
    // 且无授权时任何探测都读不到），未授权时由 UI 引导用户点「录音目录」
    final dirAccess = await _recordingService.hasDirectoryAccess();
    _recordingResult = _recordingService.result;

    if (!mounted) return;
    setState(() {
      _deviceInfo = deviceInfo;
      _hasDirAccess = dirAccess;
      _isLoading = false;
    });
  }

  /// 是否处于录音页签（录音走另一套服务）
  bool get _isRecordingTab => _tab == DeviceBackupTab.callRecording;

  /// 当前页签是否正在执行（用于禁用按钮防重复触发）
  bool get _isBusy {
    if (_isRecordingTab) {
      return _recordingResult.status == CallRecordingStatus.scanning ||
          _recordingResult.status == CallRecordingStatus.uploading ||
          _recordingService.isRunning;
    }
    return _result.status == DeviceBackupStatus.loading ||
        _result.status == DeviceBackupStatus.exporting ||
        _result.status == DeviceBackupStatus.uploading;
  }

  /// 当前页签的目标文件夹名（两类备份各有自己的配置）
  String get _targetFolderName => _isRecordingTab
      ? _recordingService.config.targetFolderName
      : _config.targetFolderName;

  /// 当前页签对应的短信/通话类型（录音页签下无意义，调用方须先判断）
  DeviceBackupType get _type =>
      _tab == DeviceBackupTab.sms ? DeviceBackupType.sms : DeviceBackupType.callLog;

  DeviceBackupResult get _result =>
      _tab == DeviceBackupTab.sms ? _smsResult : _callResult;

  DeviceBackupConfig get _config => _service.config(_type);

  Future<void> _startBackup() async {
    final al = AppLocalizations.of(context);

    if (!_isRecordingTab) {
      // 短信/通话记录：目标目录为空时后端会在上传阶段拒绝
      // （cannot upload file to root directory），故前置提示，
      // 避免用户白等一次导出+上传才发现没选目录
      if (_config.targetParentId.isEmpty) {
        await _showMissingConfigDialog([
          al.contactBackup_backupTo,
        ]);
        return;
      }
      await _service.startBackup(_type);
      return;
    }

    // 录音：先做配置自检并明确提示缺什么。
    // 原先直接调用 startBackup，配置不全时它只把状态文案改成"尚未配置"就返回，
    // 用户看到的现象是"点了备份但没有任何反应/没有上传"，无从判断原因。
    final config = _recordingService.config;
    final missing = <String>[];
    if (config.sourceDir.isEmpty) missing.add(al.callRecording_sourceDir);
    if (config.targetParentId.isEmpty) {
      missing.add(al.callRecording_targetFolder);
    }
    if (missing.isNotEmpty) {
      await _showMissingConfigDialog(missing);
      return;
    }

    final result = await _recordingService.startBackup();
    if (!mounted) return;
    setState(() => _recordingResult = result);

    // 没有待上传文件时明确告知：用户点"备份"却没动静时，
    // 需要知道是"没有新录音"而不是功能坏了
    if (result.status == CallRecordingStatus.completed &&
        result.pendingFiles == 0) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(al.callRecording_nothingToBackup(result.totalFiles)),
        ),
      );
    }
  }

  Future<void> _selectTargetFolder() async {
    final r = await Navigator.push<FolderPickResult>(
      context,
      MaterialPageRoute(builder: (context) => const FolderPickerPage()),
    );
    if (r == null) return;
    if (_isRecordingTab) {
      await _recordingService.setTargetFolder(r.id, r.name);
    } else {
      await _service.setTargetFolder(_type, r.id, r.name);
    }
    setState(() {});
  }

  /// 提示"缺少哪些配置"的弹窗（短信/通话/录音三处共用）
  Future<void> _showMissingConfigDialog(List<String> missing) async {
    if (!mounted) return;
    final al = AppLocalizations.of(context);
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.callRecording_statusNotConfigured),
        content: Text(al.callRecording_missingConfig(missing.join('、'))),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(al.common_confirm),
          ),
        ],
      ),
    );
  }

  /// 授权录音目录（SAF）。
  ///
  /// 不能用普通路径选择器：录音目录属 media_rw 组、本应用不在该组，
  /// 路径与 MediaStore 都读不到（详见 CallRecordingChannel 注释）。
  /// 必须经系统目录选择器授予 content:// 读取权限并持久化。
  Future<void> _selectSourceDir() async {
    final al = AppLocalizations.of(context);
    final ok = await _recordingService.pickDirectory();
    if (!mounted) return;
    final access = await _recordingService.hasDirectoryAccess();
    if (!mounted) return;
    setState(() {
      _hasDirAccess = access;
    });
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          ok ? al.callRecording_dirGranted : al.callRecording_dirNotGranted,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    // 桌面端显示"仅移动端可用"提示
    if (isDesktop()) {
      return Scaffold(
        appBar: AppBar(title: Text(al.deviceBackup_title), centerTitle: true),
        body: Center(
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(
                Icons.phone_android,
                size: 64,
                color: Theme.of(
                  context,
                ).colorScheme.onSurfaceVariant.withValues(alpha: 0.5),
              ),
              const SizedBox(height: 16),
              Text(
                al.contactBackup_mobileOnly,
                style: TextStyle(
                  fontSize: 18,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                al.contactBackup_mobileOnlyDesc,
                style: TextStyle(
                  fontSize: 14,
                  color: Theme.of(
                    context,
                  ).colorScheme.onSurfaceVariant.withValues(alpha: 0.5),
                ),
              ),
            ],
          ),
        ),
      );
    }

    if (_isLoading) {
      return Scaffold(
        appBar: AppBar(title: Text(al.deviceBackup_title)),
        body: const Center(child: CircularProgressIndicator()),
      );
    }

    return Scaffold(
      appBar: AppBar(title: Text(al.deviceBackup_title), centerTitle: true),
      body: Column(
        children: [
          // Tab 切换：短信 / 通话记录 / 通话录音
          TabBar(
            controller: _tabController,
            tabs: [
              Tab(text: al.deviceBackup_tabSms),
              Tab(text: al.deviceBackup_tabCallLog),
              Tab(text: al.callRecording_tab),
            ],
          ),
          Expanded(
            child: ListView(
              children: [
                if (_isRecordingTab)
                  ..._buildRecordingSections()
                else ...[
                  _buildStatusCard(),
                  _buildInfoCard(),
                ],
                ListTile(
                  title: Text(
                    _isRecordingTab
                        ? al.callRecording_targetFolder
                        : al.contactBackup_backupTo,
                  ),
                  subtitle: Text(
                    _targetFolderName.isEmpty
                        ? al.contactBackup_notSetRoot
                        : _targetFolderName,
                  ),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: _selectTargetFolder,
                ),
                if (!_isRecordingTab) _buildDeviceTile(),
                const Divider(height: 1),
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: ElevatedButton.icon(
                    onPressed: _isBusy ? null : _startBackup,
                    icon: const Icon(Icons.backup),
                    label: Text(
                      switch (_tab) {
                        DeviceBackupTab.sms => al.deviceBackup_backupSms,
                        DeviceBackupTab.callLog => al.deviceBackup_backupCallLog,
                        DeviceBackupTab.callRecording =>
                          al.callRecording_backupNow,
                      },
                    ),
                    style: ElevatedButton.styleFrom(
                      padding: const EdgeInsets.symmetric(vertical: 16),
                    ),
                  ),
                ),
                if (_isRecordingTab)
                  _buildRecordingFormatInfo()
                else
                  _buildFormatInfo(),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildStatusCard() {
    final al = AppLocalizations.of(context);
    final isSms = _tab == DeviceBackupTab.sms;
    Color statusColor;
    String statusText;
    IconData statusIcon;

    switch (_result.status) {
      case DeviceBackupStatus.idle:
        statusColor = Theme.of(
          context,
        ).colorScheme.onSurfaceVariant.withValues(alpha: 0.5);
        statusText = al.contactBackup_statusReady;
        statusIcon = isSms ? Icons.sms_outlined : Icons.call_outlined;
        break;
      case DeviceBackupStatus.loading:
        statusColor = PfmColors.primary;
        statusText = al.contactBackup_statusLoading;
        statusIcon = Icons.hourglass_empty;
        break;
      case DeviceBackupStatus.exporting:
        statusColor = PfmColors.primary;
        statusText = al.deviceBackup_statusExporting(
          _result.exportedCount,
          _result.totalCount,
        );
        statusIcon = Icons.import_export;
        break;
      case DeviceBackupStatus.uploading:
        statusColor = PfmColors.warning;
        statusText = al.contactBackup_statusUploading;
        statusIcon = Icons.cloud_upload;
        break;
      case DeviceBackupStatus.completed:
        statusColor = PfmColors.success;
        statusText = al.contactBackup_statusCompleted;
        statusIcon = Icons.check_circle;
        break;
      case DeviceBackupStatus.skipped:
        statusColor = PfmColors.success;
        statusText = al.contactBackup_statusSkipped;
        statusIcon = Icons.check_circle_outline;
        break;
      case DeviceBackupStatus.error:
        statusColor = PfmColors.error;
        statusText = al.contactBackup_statusFailed;
        statusIcon = Icons.error_outline;
        break;
      case DeviceBackupStatus.noPermission:
        statusColor = PfmColors.error;
        statusText = al.contactBackup_needPermission;
        statusIcon = Icons.lock_outline;
        break;
    }

    return Card(
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            Row(
              children: [
                Icon(statusIcon, color: statusColor, size: 48),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        statusText,
                        style: TextStyle(
                          fontSize: 18,
                          fontWeight: FontWeight.bold,
                          color: statusColor,
                        ),
                      ),
                      if (_result.errorMessage != null) ...[
                        const SizedBox(height: 4),
                        Text(
                          _result.errorMessage!,
                          style: TextStyle(
                            fontSize: 14,
                            color: Theme.of(
                              context,
                            ).colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                      if (_result.totalCount > 0) ...[
                        const SizedBox(height: 4),
                        Text(
                          al.deviceBackup_totalCount(_result.totalCount),
                          style: TextStyle(
                            fontSize: 14,
                            color: Theme.of(
                              context,
                            ).colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              ],
            ),
            if (_result.status == DeviceBackupStatus.exporting ||
                _result.status == DeviceBackupStatus.uploading) ...[
              const SizedBox(height: 16),
              const LinearProgressIndicator(),
            ],
            if (_result.lastBackupTime != null) ...[
              const SizedBox(height: 8),
              Text(
                al.contactBackup_lastBackup(_result.lastBackupTime!.toString()),
                style: TextStyle(
                  fontSize: 12,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  /// 设备标识说明行。
  ///
  /// 短信与通话记录不跨设备合并（每台手机的数据互相独立），
  /// 多设备备份到同一目录时必须标明来源，否则文件混作一谈。
  ///
  /// **必须显示实时取到的 _deviceInfo，不能显示配置里存的那个**：
  /// 旧版本存过错误的标识（华为把型号当设备名），若优先显示存量值，
  /// 用户会一直看到那个错值，误以为现在还没修好。
  Widget _buildDeviceTile() {
    final al = AppLocalizations.of(context);
    final current = _deviceInfo.isEmpty
        ? al.deviceBackup_deviceLabelHint
        : _deviceInfo;
    final lastDevice = _config.deviceLabel;
    // 上次备份的来源设备与当前设备不一致时才补充显示：
    // 换机场景下用户需要知道"这份备份记录是旧手机留下的"。
    // 排除兜底值：旧版本通道失败时会把 unknown_device 存进配置，
    // 那不是真实设备名，显示出来只会误导用户
    final showLast =
        lastDevice != null &&
        lastDevice.isNotEmpty &&
        lastDevice != kDeviceLabelFallback &&
        lastDevice != _deviceInfo;
    return ListTile(
      leading: const Icon(Icons.smartphone),
      title: Text(al.deviceBackup_deviceLabel),
      subtitle: Text(
        showLast
            ? '$current\n${al.deviceBackup_lastDevice(lastDevice)}'
            : current,
      ),
      isThreeLine: showLast,
    );
  }

  /// 录音页签的内容区块：状态卡 + 源目录行 + 说明
  List<Widget> _buildRecordingSections() {
    final al = AppLocalizations.of(context);
    return [
      _buildRecordingStatusCard(),
      ListTile(
        leading: Icon(
          _hasDirAccess ? Icons.folder_open : Icons.folder_off_outlined,
        ),
        title: Text(al.callRecording_sourceDir),
        subtitle: Text(
          _hasDirAccess
              ? (_recordingService.config.sourceDir.isEmpty
                    ? al.callRecording_sourceDirHint
                    : _recordingService.config.sourceDir)
              : al.callRecording_sourceDirHint,
        ),
        trailing: const Icon(Icons.chevron_right),
        onTap: _selectSourceDir,
      ),
      Card(
        margin: const EdgeInsets.all(16),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(Icons.info_outline, color: PfmColors.primary),
                  const SizedBox(width: 8),
                  Text(
                    al.contactBackup_instructions,
                    style: TextStyle(
                      fontWeight: FontWeight.bold,
                      color: PfmColors.primary,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              Text(al.callRecording_instr1),
              Text(al.callRecording_instr2),
              Text(al.callRecording_instr3),
            ],
          ),
        ),
      ),
    ];
  }

  /// 录音状态卡。
  ///
  /// 与短信/通话记录的区别：录音是"搬运磁盘上已有的文件"，一次可能几百个，
  /// 故展示"待上传/已建任务"的数量而非单条记录数。
  Widget _buildRecordingStatusCard() {
    final al = AppLocalizations.of(context);
    Color statusColor;
    String statusText;
    IconData statusIcon;

    switch (_recordingResult.status) {
      case CallRecordingStatus.idle:
        statusColor = Theme.of(
          context,
        ).colorScheme.onSurfaceVariant.withValues(alpha: 0.5);
        statusText = al.contactBackup_statusReady;
        statusIcon = Icons.mic_none;
        break;
      case CallRecordingStatus.scanning:
        statusColor = PfmColors.primary;
        statusText = al.callRecording_statusScanning;
        statusIcon = Icons.search;
        break;
      case CallRecordingStatus.uploading:
        statusColor = PfmColors.warning;
        statusText = al.contactBackup_statusUploading;
        statusIcon = Icons.cloud_upload;
        break;
      case CallRecordingStatus.completed:
        statusColor = PfmColors.success;
        statusText = al.contactBackup_statusCompleted;
        statusIcon = Icons.check_circle;
        break;
      case CallRecordingStatus.error:
        statusColor = PfmColors.error;
        statusText = al.contactBackup_statusFailed;
        statusIcon = Icons.error_outline;
        break;
      case CallRecordingStatus.noPermission:
        statusColor = PfmColors.error;
        statusText = al.contactBackup_needPermission;
        statusIcon = Icons.lock_outline;
        break;
      case CallRecordingStatus.notConfigured:
        statusColor = PfmColors.warning;
        statusText = al.callRecording_statusNotConfigured;
        statusIcon = Icons.settings;
        break;
    }

    return Card(
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            Row(
              children: [
                Icon(statusIcon, color: statusColor, size: 48),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        statusText,
                        style: TextStyle(
                          fontSize: 18,
                          fontWeight: FontWeight.bold,
                          color: statusColor,
                        ),
                      ),
                      if (_recordingResult.errorMessage != null) ...[
                        const SizedBox(height: 4),
                        Text(
                          _recordingResult.errorMessage!,
                          style: TextStyle(
                            fontSize: 14,
                            color: Theme.of(
                              context,
                            ).colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                      if (_recordingResult.totalFiles > 0) ...[
                        const SizedBox(height: 4),
                        Text(
                          al.callRecording_summary(
                            _recordingResult.totalFiles,
                            _recordingResult.pendingFiles,
                          ),
                          style: TextStyle(
                            fontSize: 14,
                            color: Theme.of(
                              context,
                            ).colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              ],
            ),
            if (_recordingResult.status == CallRecordingStatus.scanning ||
                _recordingResult.status == CallRecordingStatus.uploading) ...[
              const SizedBox(height: 16),
              const LinearProgressIndicator(),
            ],
            if (_recordingService.config.lastRunAt != null) ...[
              const SizedBox(height: 8),
              Text(
                al.contactBackup_lastBackup(
                  _recordingService.config.lastRunAt!.toString(),
                ),
                style: TextStyle(
                  fontSize: 12,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  /// 录音页签的"导出格式/恢复方式"说明
  Widget _buildRecordingFormatInfo() {
    final al = AppLocalizations.of(context);
    return Card(
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.description_outlined,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 8),
                Text(
                  al.contactBackup_howToRestore,
                  style: TextStyle(
                    fontWeight: FontWeight.bold,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(al.callRecording_restoreStep1),
          ],
        ),
      ),
    );
  }

  Widget _buildInfoCard() {
    final al = AppLocalizations.of(context);
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.info_outline, color: PfmColors.primary),
                const SizedBox(width: 8),
                Text(
                  al.contactBackup_instructions,
                  style: TextStyle(
                    fontWeight: FontWeight.bold,
                    color: PfmColors.primary,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(al.deviceBackup_instr1),
            Text(al.deviceBackup_instr2),
            Text(al.deviceBackup_instr3),
            Text(al.deviceBackup_instr4),
          ],
        ),
      ),
    );
  }

  Widget _buildFormatInfo() {
    final al = AppLocalizations.of(context);
    return Card(
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.description_outlined,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 8),
                Text(
                  al.contactBackup_howToRestore,
                  style: TextStyle(
                    fontWeight: FontWeight.bold,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(al.deviceBackup_restoreStep1),
            Text(al.deviceBackup_restoreStep2),
            const SizedBox(height: 8),
            Text(
              al.contactRestore_supportedPlatforms,
              style: TextStyle(
                fontSize: 12,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
