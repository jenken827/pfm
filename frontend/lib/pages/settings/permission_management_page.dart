import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/proto/native/permission.pb.dart';
import 'package:pfm/proto/native/provider.pb.dart';
import 'package:pfm/utils/format.dart';

/// 权限管理页面（管理员专用）
/// 功能：查看用户列表 + 管理用户存储配额
class PermissionManagementPage extends StatefulWidget {
  const PermissionManagementPage({super.key});

  @override
  State<PermissionManagementPage> createState() =>
      _PermissionManagementPageState();
}

class _PermissionManagementPageState extends State<PermissionManagementPage> {
  late AppLocalizations al;
  final FmRepository _repo = FmRepository();

  List<UserInfo> _users = [];
  Map<String, UserSpace> _spaceMap = {}; // userId -> UserSpace
  List<StorageProvider> _providers = [];
  bool _isLoading = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _loadData();
  }

  Future<void> _loadData() async {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    try {
      // 并行获取用户列表、空间列表和存储提供者
      final usersResult = await _repo.listUsersNew();
      final spacesResult = await _repo.listUserSpaces(pageSize: 200);
      final providersResult = await _repo.listStorageProviders();

      if (!usersResult.success) {
        setState(() {
          _isLoading = false;
          _errorMessage = usersResult.message ?? '获取用户列表失败';
        });
        return;
      }

      final users = usersResult.data?.users.toList() ?? <UserInfo>[];
      final spaces = spacesResult.data?.spaces.toList() ?? <UserSpace>[];
      final spaceMap = <String, UserSpace>{};
      for (final s in spaces) {
        spaceMap[s.userId] = s;
      }

      final providers = providersResult.success
          ? (providersResult.data?.providers.toList() ?? <StorageProvider>[])
          : <StorageProvider>[];

      if (mounted) {
        setState(() {
          _users = users;
          _spaceMap = spaceMap;
          _providers = providers;
          _isLoading = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isLoading = false;
          _errorMessage = '加载失败: $e';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    al = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Text(al.permission_title),
        centerTitle: true,
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: _loadData,
            tooltip: al.permission_refresh,
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_errorMessage != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              _errorMessage!,
              style: const TextStyle(color: PfmColors.error),
            ),
            const SizedBox(height: 16),
            ElevatedButton(
              onPressed: _loadData,
              child: Text(al.permission_retry),
            ),
          ],
        ),
      );
    }

    if (_users.isEmpty) {
      return Center(
        child: Text(
          '暂无用户',
          style: TextStyle(
            color: Theme.of(context).colorScheme.onSurfaceVariant,
            fontSize: 16,
          ),
        ),
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.symmetric(vertical: 8),
      itemCount: _users.length,
      itemBuilder: (context, index) {
        final user = _users[index];
        final space = _spaceMap[user.userId];
        return _buildUserTile(user, space);
      },
    );
  }

  Widget _buildUserTile(UserInfo user, UserSpace? space) {
    final colorScheme = Theme.of(context).colorScheme;
    final quotaBytes = space?.quotaBytes.toInt() ?? 0;
    final usedBytes = space?.usedBytes.toInt() ?? 0;
    final isUnlimited = quotaBytes == 0;
    final usageRatio = isUnlimited
        ? 0.0
        : (usedBytes / quotaBytes).clamp(0.0, 1.0);

    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          children: [
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: CircleAvatar(
                backgroundColor: colorScheme.primaryContainer,
                child: Text(
                  user.displayName.isNotEmpty
                      ? user.displayName[0].toUpperCase()
                      : '?',
                  style: TextStyle(
                    color: colorScheme.onPrimaryContainer,
                    fontWeight: FontWeight.bold,
                  ),
                ),
              ),
              title: Text(
                user.displayName.isNotEmpty ? user.displayName : user.userId,
                style: const TextStyle(fontWeight: FontWeight.w600),
              ),
              subtitle: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (user.email.isNotEmpty)
                    Text(
                      user.email,
                      style: TextStyle(
                        fontSize: 12,
                        color: colorScheme.onSurfaceVariant,
                      ),
                    ),
                  if (user.roles.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Wrap(
                        spacing: 4,
                        children: user.roles
                            .map(
                              (r) => Chip(
                                label: Text(
                                  r,
                                  style: const TextStyle(fontSize: 10),
                                ),
                                padding: EdgeInsets.zero,
                                materialTapTargetSize:
                                    MaterialTapTargetSize.shrinkWrap,
                                visualDensity: VisualDensity.compact,
                              ),
                            )
                            .toList(),
                      ),
                    ),
                ],
              ),
              trailing: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  IconButton(
                    icon: const Icon(Icons.folder_special, size: 20),
                    tooltip: '分配根目录',
                    onPressed: () => _showAssignRootDirDialog(user, space),
                  ),
                  IconButton(
                    icon: const Icon(Icons.edit, size: 20),
                    tooltip: '设置配额',
                    onPressed: () => _showQuotaDialog(user, space),
                  ),
                ],
              ),
            ),
            // 已绑定的存储提供者
            if (space != null && space.boundProviderIds.isNotEmpty) ...[
              const SizedBox(height: 6),
              _buildBoundProvidersInfo(space.boundProviderIds),
            ],
            // 存储用量条
            if (space != null) ...[
              const SizedBox(height: 8),
              _buildStorageBar(usedBytes, quotaBytes, isUnlimited, usageRatio),
            ] else ...[
              const SizedBox(height: 4),
              Text(
                '用户空间未初始化',
                style: TextStyle(
                  fontSize: 12,
                  color: colorScheme.onSurfaceVariant.withValues(alpha: 0.6),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _buildStorageBar(
    int usedBytes,
    int quotaBytes,
    bool isUnlimited,
    double ratio,
  ) {
    final colorScheme = Theme.of(context).colorScheme;
    final usedStr = formatCapacityString(usedBytes);
    final quotaStr = isUnlimited ? '不限' : formatCapacityString(quotaBytes);
    final barColor = ratio > 0.9
        ? PfmColors.error
        : ratio > 0.7
        ? PfmColors.warning
        : colorScheme.primary;

    return Column(
      children: [
        if (!isUnlimited) ...[
          ClipRRect(
            borderRadius: BorderRadius.circular(4),
            child: LinearProgressIndicator(
              value: ratio,
              minHeight: 6,
              backgroundColor: colorScheme.surfaceContainerHighest,
              valueColor: AlwaysStoppedAnimation<Color>(barColor),
            ),
          ),
          const SizedBox(height: 4),
        ],
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            Text(
              '已用 $usedStr',
              style: TextStyle(
                fontSize: 11,
                color: colorScheme.onSurfaceVariant,
              ),
            ),
            Text(
              isUnlimited ? '配额: 不限' : '配额: $quotaStr',
              style: TextStyle(
                fontSize: 11,
                color: colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ],
    );
  }

  void _showQuotaDialog(UserInfo user, UserSpace? space) {
    final currentQuota = space?.quotaBytes.toInt() ?? 0;
    final controller = TextEditingController(
      text: currentQuota > 0
          ? (currentQuota / (1024 * 1024 * 1024)).toStringAsFixed(1)
          : '',
    );
    bool isUnlimited = currentQuota == 0;

    showDialog(
      context: context,
      builder: (ctx) {
        return StatefulBuilder(
          builder: (ctx, setDialogState) {
            return AlertDialog(
              title: Text('设置配额 - ${user.userId}'),
              content: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  // 当前用量
                  if (space != null)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 16),
                      child: Text(
                        '当前已用: ${formatCapacityString(space.usedBytes.toInt())}',
                        style: TextStyle(
                          color: Theme.of(ctx).colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                  // 不限配额开关
                  SwitchListTile(
                    title: const Text('不限配额'),
                    value: isUnlimited,
                    onChanged: (v) {
                      setDialogState(() => isUnlimited = v);
                    },
                    contentPadding: EdgeInsets.zero,
                  ),
                  if (!isUnlimited) ...[
                    const SizedBox(height: 8),
                    TextField(
                      controller: controller,
                      keyboardType: const TextInputType.numberWithOptions(
                        decimal: true,
                      ),
                      decoration: const InputDecoration(
                        labelText: '配额 (GB)',
                        hintText: '例如: 10.0',
                        border: OutlineInputBorder(),
                      ),
                    ),
                  ],
                ],
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(ctx),
                  child: Text(AppLocalizations.of(context).common_cancel),
                ),
                ElevatedButton(
                  onPressed: () async {
                    int quotaBytes;
                    if (isUnlimited) {
                      quotaBytes = 0;
                    } else {
                      final gb = double.tryParse(controller.text.trim());
                      if (gb == null || gb <= 0) {
                        ScaffoldMessenger.of(context).showSnackBar(
                          const SnackBar(content: Text('请输入有效的配额值')),
                        );
                        return;
                      }
                      quotaBytes = (gb * 1024 * 1024 * 1024).toInt();
                    }

                    Navigator.pop(ctx);
                    await _updateQuota(user.userId, quotaBytes);
                  },
                  child: const Text('保存'),
                ),
              ],
            );
          },
        );
      },
    );
  }

  Widget _buildBoundProvidersInfo(String boundProviderIdsJson) {
    final colorScheme = Theme.of(context).colorScheme;
    List<int> providerIds = [];
    try {
      providerIds = (jsonDecode(boundProviderIdsJson) as List).cast<int>();
    } catch (_) {}

    if (providerIds.isEmpty) return const SizedBox.shrink();

    final names = providerIds.map((id) {
      final p = _providers.where((p) => p.id == id);
      return p.isNotEmpty ? p.first.name : 'ID:$id';
    }).toList();

    return Wrap(
      spacing: 4,
      runSpacing: 2,
      children: [
        Icon(Icons.cloud_done, size: 14, color: colorScheme.onSurfaceVariant),
        ...names.map(
          (n) => Chip(
            label: Text(n, style: const TextStyle(fontSize: 10)),
            padding: EdgeInsets.zero,
            materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
            visualDensity: VisualDensity.compact,
          ),
        ),
      ],
    );
  }

  void _showAssignRootDirDialog(UserInfo user, UserSpace? space) {
    final hasExistingRoot = space != null && space.rootFileId.isNotEmpty;
    // 预选已绑定的 provider
    List<int> selectedIds = [];
    if (space != null && space.boundProviderIds.isNotEmpty) {
      try {
        selectedIds = (jsonDecode(space.boundProviderIds) as List).cast<int>();
      } catch (_) {}
    }

    showDialog(
      context: context,
      builder: (ctx) {
        return StatefulBuilder(
          builder: (ctx, setDialogState) {
            return AlertDialog(
              title: Text('分配根目录 - ${user.userId}'),
              content: SizedBox(
                width: 400,
                child: SingleChildScrollView(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      if (hasExistingRoot) ...[
                        Text(
                          '当前根目录: ${space.rootFileId}',
                          style: TextStyle(
                            fontSize: 12,
                            color: Theme.of(ctx).colorScheme.onSurfaceVariant,
                          ),
                        ),
                        const SizedBox(height: 4),
                        Text(
                          '重新分配将更新绑定的存储提供者',
                          style: TextStyle(
                            fontSize: 12,
                            color: Theme.of(ctx).colorScheme.error,
                          ),
                        ),
                        const SizedBox(height: 16),
                      ],
                      Text(
                        '选择存储提供者',
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                      const SizedBox(height: 8),
                      if (_providers.isEmpty)
                        const Text('暂无可用的存储提供者')
                      else
                        ..._providers.map((provider) {
                          final id = provider.id.toInt();
                          final isSelected = selectedIds.contains(id);
                          return CheckboxListTile(
                            title: Text(provider.name),
                            subtitle: Text(provider.enabled ? '已启用' : '已禁用'),
                            value: isSelected,
                            enabled: provider.enabled,
                            onChanged: (checked) {
                              setDialogState(() {
                                if (checked == true) {
                                  selectedIds.add(id);
                                } else {
                                  selectedIds.remove(id);
                                }
                              });
                            },
                            dense: true,
                            controlAffinity: ListTileControlAffinity.leading,
                            contentPadding: EdgeInsets.zero,
                          );
                        }),
                    ],
                  ),
                ),
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(ctx),
                  child: const Text('取消'),
                ),
                ElevatedButton(
                  onPressed: selectedIds.isEmpty
                      ? null
                      : () async {
                          Navigator.pop(ctx);
                          await _assignRootDir(user.userId, selectedIds);
                        },
                  child: const Text('确认分配'),
                ),
              ],
            );
          },
        );
      },
    );
  }

  Future<void> _assignRootDir(String userId, List<int> providerIds) async {
    final result = await _repo.assignRootDir(
      userId: userId,
      providerIds: providerIds,
    );
    if (result.success) {
      if (mounted) {
        final rootId = result.data?.rootFileId ?? '';
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('根目录已分配: $rootId')));
      }
      _loadData();
    } else {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('分配失败: ${result.message}')));
      }
    }
  }

  Future<void> _updateQuota(String userId, int quotaBytes) async {
    final result = await _repo.updateUserSpace(
      userId: userId,
      quotaBytes: quotaBytes,
    );
    if (result.success) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('配额已更新')));
      }
      _loadData(); // 刷新列表
    } else {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('更新失败: ${result.message}')));
      }
    }
  }
}
