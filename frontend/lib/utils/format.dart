/// 容量大小单位
enum CapacityUnit {
  b('B', 1),
  kb('KB', 1024),
  mb('MB', 1024 * 1024),
  gb('GB', 1024 * 1024 * 1024),
  tb('TB', 1024 * 1024 * 1024 * 1024),
  pb('PB', 1024 * 1024 * 1024 * 1024 * 1024);

  final String label;
  final int bytes;

  const CapacityUnit(this.label, this.bytes);
}

/// 容量格式化结果
class CapacityFormatResult {
  final double value;
  final CapacityUnit unit;

  const CapacityFormatResult(this.value, this.unit);

  /// 格式化为字符串，如 "100.5 GB"
  @override
  String toString({int decimals = 1}) {
    if (value == value.toInt()) {
      return '${value.toInt()} ${unit.label}';
    }
    return '${value.toStringAsFixed(decimals)} ${unit.label}';
  }
}

/// 格式化容量大小
///
/// 将字节数转换为易读格式（如 KB, MB, GB 等）
/// [bytes] 字节数
/// [decimals] 保留小数位数，默认为 1
/// 返回格式化结果，包含数值和单位
CapacityFormatResult formatCapacity(int bytes, {int decimals = 1}) {
  if (bytes <= 0) {
    return const CapacityFormatResult(0, CapacityUnit.b);
  }

  // 找到最合适的单位
  CapacityUnit suitableUnit = CapacityUnit.b;
  for (final unit in CapacityUnit.values) {
    if (bytes >= unit.bytes) {
      suitableUnit = unit;
    } else {
      break;
    }
  }

  final value = bytes / suitableUnit.bytes;
  return CapacityFormatResult(value, suitableUnit);
}

/// 格式化容量大小为字符串
///
/// [bytes] 字节数
/// [decimals] 保留小数位数，默认为 1
String formatCapacityString(int bytes, {int decimals = 1}) {
  final result = formatCapacity(bytes, decimals: decimals);
  return result.toString(decimals: decimals);
}

/// 从数值和单位字符串解析字节数
///
/// [value] 数值
/// [unit] 单位字符串（如 'KB', 'MB', 'GB' 等）
int parseCapacityFromUnit(num value, String unit) {
  final upperUnit = unit.toUpperCase();
  for (final capacityUnit in CapacityUnit.values) {
    if (capacityUnit.label == upperUnit) {
      return (value * capacityUnit.bytes).toInt();
    }
  }
  return value.toInt();
}
