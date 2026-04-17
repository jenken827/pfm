import 'dart:io';

/// 文件工具类
class FileUtils {
  /// 生成唯一的文件路径
  /// 
  /// 如果文件已存在，自动添加数字后缀
  /// 例如：file.txt → file (1).txt → file (2).txt
  static Future<String> getUniqueFilePath(String basePath) async {
    final file = File(basePath);
    
    // 如果文件不存在，直接返回原路径
    if (!await file.exists()) {
      return basePath;
    }
    
    // 分离文件名和扩展名
    final lastSeparator = basePath.lastIndexOf(Platform.pathSeparator);
    final dir = basePath.substring(0, lastSeparator);
    final fileName = basePath.substring(lastSeparator + 1);
    
    final lastDotIndex = fileName.lastIndexOf('.');
    String baseName;
    String extension = '';
    
    if (lastDotIndex > 0) {
      baseName = fileName.substring(0, lastDotIndex);
      extension = fileName.substring(lastDotIndex); // 包含点号
    } else {
      baseName = fileName;
    }
    
    // 尝试添加数字后缀
    int counter = 1;
    while (true) {
      final newName = '$baseName ($counter)$extension';
      final newPath = '$dir${Platform.pathSeparator}$newName';
      final newFile = File(newPath);
      
      if (!await newFile.exists()) {
        return newPath;
      }
      
      counter++;
      
      // 防止无限循环
      if (counter > 10000) {
        // 使用时间戳作为后备方案
        final timestamp = DateTime.now().millisecondsSinceEpoch;
        return '$dir${Platform.pathSeparator}${baseName}_$timestamp$extension';
      }
    }
  }
}
