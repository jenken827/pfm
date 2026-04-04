import 'package:flutter/material.dart';
import 'package:flutter_form_builder/flutter_form_builder.dart';
import 'package:form_builder_validators/form_builder_validators.dart';
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
    
    default:
      return const SizedBox.shrink();
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
  };
  return iconMap[iconName] ?? Icons.info;
}
