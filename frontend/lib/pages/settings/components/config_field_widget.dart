import 'package:flutter/material.dart';
import 'package:flutter_form_builder/flutter_form_builder.dart';
import 'package:form_builder_validators/form_builder_validators.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/provider.pb.dart';

/// 构建配置字段Widget
Widget buildConfigField(
  ConfigField field,
  Map<String, dynamic> existingConfig,
) {
  final initialValue = existingConfig[field.name];
  
  switch (field.fieldType) {
    case ConfigFieldType.STRING:
      return Padding(
        padding: const EdgeInsets.only(bottom: 16),
        child: FormBuilderTextField(
          name: field.name,
          decoration: InputDecoration(
            labelText: field.label,
            hintText: field.placeholder.isNotEmpty ? field.placeholder : null,
            helperText: field.helperText.isNotEmpty ? field.helperText : null,
            prefixIcon: field.iconName.isNotEmpty 
                ? Icon(_getIconData(field.iconName)) 
                : null,
          ),
          initialValue: initialValue?.toString(),
          maxLines: field.multilineRows > 0 ? field.multilineRows : 1,
          validator: field.required == true
              ? FormBuilderValidators.required(errorText: '请输入${field.label}')
              : null,
          obscureText: field.inputType == 'password',
        ),
      );
    
    case ConfigFieldType.NUMBER:
      return Padding(
        padding: const EdgeInsets.only(bottom: 16),
        child: FormBuilderTextField(
          name: field.name,
          decoration: InputDecoration(
            labelText: field.label,
            hintText: field.placeholder.isNotEmpty ? field.placeholder : null,
            helperText: field.helperText.isNotEmpty ? field.helperText : null,
            prefixIcon: field.iconName.isNotEmpty 
                ? Icon(_getIconData(field.iconName)) 
                : null,
          ),
          keyboardType: TextInputType.number,
          initialValue: initialValue?.toString(),
          validator: field.required == true
              ? FormBuilderValidators.required(errorText: '请输入${field.label}')
              : null,
        ),
      );
    
    case ConfigFieldType.BOOLEAN:
      return Padding(
        padding: const EdgeInsets.only(bottom: 16),
        child: FormBuilderSwitch(
          name: field.name,
          title: Text(field.label),
          subtitle: field.helperText.isNotEmpty == true 
              ? Text(field.helperText) 
              : null,
          initialValue: initialValue == true,
        ),
      );
    
    case ConfigFieldType.SELECT:
      return Padding(
        padding: const EdgeInsets.only(bottom: 16),
        child: FormBuilderDropdown<String>(
          name: field.name,
          decoration: InputDecoration(
            labelText: field.label,
            helperText: field.helperText.isNotEmpty ? field.helperText : null,
            prefixIcon: field.iconName.isNotEmpty 
                ? Icon(_getIconData(field.iconName)) 
                : null,
          ),
          initialValue: initialValue?.toString(),
          items: field.selectOptions.map((option) {
            return DropdownMenuItem(
              value: option.value,
              child: Text(option.label),
            );
          }).toList(),
          validator: field.required == true
              ? FormBuilderValidators.required(errorText: '请选择${field.label}')
              : null,
        ),
      );
    
    case ConfigFieldType.STRING_LIST:
      // 解析初始值
      List<String> initialList = [];
      if (initialValue != null) {
        if (initialValue is List) {
          initialList = initialValue.map((e) => e.toString()).toList();
        } else if (initialValue.toString().isNotEmpty) {
          initialList = [initialValue.toString()];
        }
      }
      return _StringListField(
        name: field.name,
        label: field.label,
        placeholder: field.placeholder,
        helperText: field.helperText,
        iconName: field.iconName,
        required: field.required,
        initialValue: initialList,
      );
    
    default:
      return const SizedBox.shrink();
  }
}

/// 字符串列表字段组件
/// 每行一个值，支持动态添加/删除行
class _StringListField extends StatefulWidget {
  final String name;
  final String label;
  final String placeholder;
  final String helperText;
  final String iconName;
  final bool required;
  final List<String> initialValue;

  const _StringListField({
    required this.name,
    required this.label,
    this.placeholder = '',
    this.helperText = '',
    this.iconName = '',
    this.required = false,
    this.initialValue = const [],
  });

  @override
  State<_StringListField> createState() => _StringListFieldState();
}

class _StringListFieldState extends State<_StringListField> {
  late List<String> _items;
  late List<TextEditingController> _controllers;
  late List<FocusNode> _focusNodes;

  @override
  void initState() {
    super.initState();
    _items = List.from(widget.initialValue);
    _controllers = _items.map((e) => TextEditingController(text: e)).toList();
    _focusNodes = _items.map((_) => FocusNode()).toList();
    // 如果没有初始项，添加一个空行（不自动聚焦，避免切换类型时光标跳转）
    if (_controllers.isEmpty) {
      _addRow(autoFocus: false);
    }
  }

  @override
  void dispose() {
    for (final c in _controllers) {
      c.dispose();
    }
    for (final f in _focusNodes) {
      f.dispose();
    }
    super.dispose();
  }

  void _addRow({bool autoFocus = true}) {
    setState(() {
      _items.add('');
      _controllers.add(TextEditingController());
      _focusNodes.add(FocusNode());
    });
    // 聚焦到新添加的行
    if (autoFocus) {
      Future.microtask(() {
        if (_focusNodes.isNotEmpty) {
          _focusNodes.last.requestFocus();
        }
      });
    }
  }

  void _removeRow(int index) {
    setState(() {
      _controllers[index].dispose();
      _focusNodes[index].dispose();
      _controllers.removeAt(index);
      _focusNodes.removeAt(index);
      _items.removeAt(index);
    });
    // 至少保留一行
    if (_controllers.isEmpty) {
      _addRow();
    }
  }

  List<String> _getValues() {
    return _controllers
        .map((c) => c.text.trim())
        .where((s) => s.isNotEmpty)
        .toList();
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: FormBuilderField<List<String>>(
        name: widget.name,
        initialValue: widget.initialValue,
        builder: (field) {
          return InputDecorator(
            decoration: InputDecoration(
              labelText: widget.label,
              helperText: widget.helperText.isNotEmpty ? widget.helperText : null,
              prefixIcon: widget.iconName.isNotEmpty 
                  ? Icon(_getIconData(widget.iconName)) 
                  : null,
              errorText: field.errorText,
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                ...List.generate(_controllers.length, (index) {
                  return Padding(
                    padding: const EdgeInsets.symmetric(vertical: 4),
                    child: Row(
                      children: [
                        Expanded(
                          child: TextField(
                            controller: _controllers[index],
                            focusNode: _focusNodes[index],
                            decoration: InputDecoration(
                              hintText: widget.placeholder.isNotEmpty 
                                  ? widget.placeholder 
                                  : null,
                              isDense: true,
                              contentPadding: const EdgeInsets.symmetric(
                                horizontal: 12,
                                vertical: 10,
                              ),
                              border: const OutlineInputBorder(),
                            ),
                            onChanged: (value) {
                              _items[index] = value;
                              field.didChange(_getValues());
                            },
                          ),
                        ),
                        const SizedBox(width: 8),
                        IconButton(
                          icon: const Icon(Icons.remove_circle_outline, size: 20),
                          onPressed: _controllers.length > 1
                              ? () => _removeRow(index)
                              : null,
                          tooltip: al.common_delete,
                          padding: EdgeInsets.zero,
                          constraints: const BoxConstraints(),
                        ),
                      ],
                    ),
                  );
                }),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    onPressed: _addRow,
                    icon: const Icon(Icons.add, size: 18),
                    label: Text(al.common_add),
                    style: TextButton.styleFrom(
                      padding: const EdgeInsets.symmetric(horizontal: 8),
                      minimumSize: Size.zero,
                      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                    ),
                  ),
                ),
              ],
            ),
          );
        },
        validator: widget.required
            ? (value) {
                final values = _getValues();
                if (values.isEmpty) {
                  return '请输入${widget.label}';
                }
                return null;
              }
            : null,
      ),
    );
  }
}

/// 获取图标数据
IconData _getIconData(String iconName) {
  final iconMap = {
    'folder_open': Icons.folder_open,
    'security': Icons.security,
    'send': Icons.send,
    'vpn_key': Icons.vpn_key,
    'extension': Icons.extension,
    'storage': Icons.storage,
    'label': Icons.label,
    'description': Icons.description,
    'sort': Icons.sort,
    'category': Icons.category,
    'cloud': Icons.cloud,
    'lock': Icons.lock,
    'chat': Icons.chat,
    'webhook': Icons.sync_alt,
  };
  return iconMap[iconName] ?? Icons.info;
}
