import 'package:flutter/material.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/proto/native/permission.pb.dart';
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

  List<CasdoorUserInfo> _users = [];
  Map<String, UserSpace> _spaceMap = {}; // userId -> UserSpace
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
      // 并行获取用户列表和空间列表
      final usersResult = await _repo.listUsersNew();
      final spacesResult = await _repo.listUserSpaces(pageSize: 200);

      if (!usersResult.success) {
        setState(() {
          _isLoading = false;
          _errorMessage = usersResult.message ?? '获取用户列表失败';
        });
        return;
      }

      final users = usersResult.data?.users.toList() ?? <CasdoorUserInfo>[];
      final spaces = spacesResult.data?.spaces.toList() ?? <UserSpace>[];
      final spaceMap = <String, UserSpace>{};
      for (final s in spaces) {
        spaceMap[s.userId] = s;
      }

      if (mounted) {
        setState(() {
          _users = users;
          _spaceMap = spaceMap;
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
    al = AppLocalizations.of(context)!;
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
            Text(_errorMessage!, style: const TextStyle(color: PfmColors.error)),
            const SizedBox(height: 16),
            ElevatedButton(onPressed: _loadData, child: Text(al.permission_retry)),
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

  Widget _buildUserTile(CasdoorUserInfo user, UserSpace? space) {
    final colorScheme = Theme.of(context).colorScheme;
    final quotaBytes = space?.quotaBytes.toInt() ?? 0;
    final usedBytes = space?.usedBytes.toInt() ?? 0;
    final isUnlimited = quotaBytes == 0;
    final usageRatio = isUnlimited ? 0.0 : (usedBytes / quotaBytes).clamp(0.0, 1.0);

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
                  user.userId.isNotEmpty ? user.userId[0].toUpperCase() : '?',
                  style: TextStyle(
                    color: colorScheme.onPrimaryContainer,
                    fontWeight: FontWeight.bold,
                  ),
                ),
              ),
              title: Text(
                user.userId,
                style: const TextStyle(fontWeight: FontWeight.w600),
              ),
              subtitle: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (user.email.isNotEmpty)
                    Text(user.email,
                        style: TextStyle(
                            fontSize: 12,
                            color: colorScheme.onSurfaceVariant)),
                  if (user.roles.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Wrap(
                        spacing: 4,
                        children: user.roles
                            .map((r) => Chip(
                                  label: Text(r,
                                      style: const TextStyle(fontSize: 10)),
                                  padding: EdgeInsets.zero,
                                  materialTapTargetSize:
                                      MaterialTapTargetSize.shrinkWrap,
                                  visualDensity: VisualDensity.compact,
                                ))
                            .toList(),
                      ),
                    ),
                ],
              ),
              trailing: IconButton(
                icon: const Icon(Icons.edit, size: 20),
                tooltip: '设置配额',
                onPressed: () => _showQuotaDialog(user, space),
              ),
            ),
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
      int usedBytes, int quotaBytes, bool isUnlimited, double ratio) {
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
        ClipRRect(
          borderRadius: BorderRadius.circular(4),
          child: LinearProgressIndicator(
            value: isUnlimited ? null : ratio,
            minHeight: 6,
            backgroundColor: colorScheme.surfaceContainerHighest,
            valueColor: AlwaysStoppedAnimation<Color>(barColor),
          ),
        ),
        const SizedBox(height: 4),
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

  void _showQuotaDialog(CasdoorUserInfo user, UserSpace? space) {
    final currentQuota = space?.quotaBytes.toInt() ?? 0;
    final controller = TextEditingController(
      text: currentQuota > 0 ? (currentQuota / (1024 * 1024 * 1024)).toStringAsFixed(1) : '',
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
                      keyboardType: const TextInputType.numberWithOptions(decimal: true),
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

  Future<void> _updateQuota(String userId, int quotaBytes) async {
    final result = await _repo.updateUserSpace(
      userId: userId,
      quotaBytes: quotaBytes,
    );
    if (result.success) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('配额已更新')),
        );
      }
      _loadData(); // 刷新列表
    } else {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('更新失败: ${result.message}')),
        );
      }
    }
  }
}
