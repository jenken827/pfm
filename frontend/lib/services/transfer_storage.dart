import 'package:fixnum/fixnum.dart';
import 'package:hive_ce/hive.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/services/logger.dart';

/// Hive-based storage for upload/download tasks.
///
/// Uses per-key locking instead of database-level locking,
/// eliminating contention under high concurrency (8+ uploads).
/// backup_file_records remains in Sqlite3Storage (complex queries).
class TransferStorage {
  static const _uploadBoxName = 'upload_tasks';
  static const _downloadBoxName = 'download_tasks';

  static TransferStorage? _instance;
  late Box<Map> _uploadBox;
  late Box<Map> _downloadBox;

  TransferStorage._();

  static Future<TransferStorage> instance() async {
    if (_instance != null) return _instance!;
    _instance = TransferStorage._();
    _instance!._uploadBox = await Hive.openBox<Map>(_uploadBoxName);
    _instance!._downloadBox = await Hive.openBox<Map>(_downloadBoxName);
    getLogger().info(
      'TransferStorage initialized (Hive) '
      'upload=${_instance!._uploadBox.length}, '
      'download=${_instance!._downloadBox.length}',
    );
    return _instance!;
  }

  // ==================== Upload Tasks ====================

  Future<void> addUploadTask(UploadTaskRecord task) async {
    await _uploadBox.put(task.id, task.toJson());
  }

  Future<void> addUploadTasksBatch(List<UploadTaskRecord> tasks) async {
    final map = <String, Map>{for (final t in tasks) t.id: t.toJson()};
    await _uploadBox.putAll(map);
  }

  Future<void> updateUploadTask(
    String id, {
    UploadStatus? status,
    String? errorMessage,
    int? chunkTotal,
    Int64? chunkSize,
    String? uploadId,
    int? retryCount,
    int? uploadedBytes,
    String? fileHash,
    int? speed,
    String? filename,
  }) async {
    final raw = _uploadBox.get(id);
    if (raw == null) return;
    final existing = Map<String, dynamic>.from(raw);

    if (status != null) {
      existing['status'] = status.value;
      if (status == UploadStatus.COMPLETED) {
        existing['completed_at'] = DateTime.now().toIso8601String();
      }
    }
    if (errorMessage != null) existing['error_message'] = errorMessage;
    if (uploadId != null) existing['upload_id'] = uploadId;
    if (retryCount != null) existing['retry_count'] = retryCount;
    if (uploadedBytes != null) existing['uploaded_bytes'] = uploadedBytes;
    if (fileHash != null) existing['file_hash'] = fileHash;
    if (speed != null) existing['speed'] = speed;
    if (chunkTotal != null) existing['chunk_total'] = chunkTotal;
    if (chunkSize != null) existing['chunk_size'] = chunkSize.toInt();
    if (filename != null) existing['filename'] = filename;

    await _uploadBox.put(id, existing);
  }

  Future<UploadTaskRecord?> getUploadTaskAsync(String id) async {
    final raw = _uploadBox.get(id);
    if (raw == null) return null;
    return UploadTaskRecord.fromJson(Map<String, dynamic>.from(raw));
  }

  Future<List<UploadTaskRecord>> getAllUploadTasksAsync(
      {int limit = 100}) async {
    final all = _uploadBox.values
        .map((m) => UploadTaskRecord.fromJson(Map<String, dynamic>.from(m)))
        .toList()
      ..sort((a, b) => b.createdAt.compareTo(a.createdAt));
    return all.take(limit).toList();
  }

  Future<List<UploadTaskRecord>> getUploadTasksByStatus(
    UploadStatus status, {
    int limit = 100,
    int offset = 0,
  }) async {
    final filtered = _uploadBox.values
        .map((m) => UploadTaskRecord.fromJson(Map<String, dynamic>.from(m)))
        .where((t) => t.status == status)
        .toList()
      ..sort((a, b) => b.createdAt.compareTo(a.createdAt));
    if (offset > 0) filtered.removeRange(0, offset.clamp(0, filtered.length));
    return filtered.take(limit).toList();
  }

  Future<Map<String, int>> countAllUploadTasks() async {
    int uploading = 0, pending = 0, completed = 0, failed = 0;
    for (final raw in _uploadBox.values) {
      final s = (raw as Map)['status'] as int? ?? 0;
      if (s == UploadStatus.UPLOADING.value ||
          s == UploadStatus.HASHING.value) {
        uploading++;
      } else if (s == UploadStatus.PENDING.value) {
        pending++;
      } else if (s == UploadStatus.COMPLETED.value) {
        completed++;
      } else if (s == UploadStatus.FAILED.value) {
        failed++;
      }
    }
    return {
      'uploading': uploading,
      'pending': pending,
      'completed': completed,
      'failed': failed,
    };
  }

  Future<UploadTaskRecord?> getNextPendingUploadTaskAsync() async {
    UploadTaskRecord? earliest;
    for (final raw in _uploadBox.values) {
      final t = UploadTaskRecord.fromJson(Map<String, dynamic>.from(raw));
      if (t.status == UploadStatus.PENDING) {
        if (earliest == null || t.createdAt.isBefore(earliest.createdAt)) {
          earliest = t;
        }
      }
    }
    return earliest;
  }

  Future<int> resetStuckUploadTasks() async {
    int count = 0;
    for (final key in _uploadBox.keys) {
      final raw = _uploadBox.get(key);
      if (raw == null) continue;
      final m = Map<String, dynamic>.from(raw);
      final s = m['status'] as int? ?? 0;
      if (s == UploadStatus.UPLOADING.value ||
          s == UploadStatus.HASHING.value) {
        m['status'] = UploadStatus.PENDING.value;
        m['upload_id'] = null;
        await _uploadBox.put(key, m);
        count++;
      }
    }
    return count;
  }

  Future<void> deleteUploadTask(String? id) async {
    if (id == null) {
      await _uploadBox.clear();
    } else {
      await _uploadBox.delete(id);
    }
  }

  Future<void> clearUploadCompleted() async {
    final keysToDelete = <dynamic>[];
    for (final key in _uploadBox.keys) {
      final raw = _uploadBox.get(key);
      if (raw == null) continue;
      final s = (raw as Map)['status'] as int? ?? 0;
      if (s == UploadStatus.COMPLETED.value) {
        keysToDelete.add(key);
      }
    }
    await _uploadBox.deleteAll(keysToDelete);
  }

  Future<void> clearUploadFailed() async {
    final keysToDelete = <dynamic>[];
    for (final key in _uploadBox.keys) {
      final raw = _uploadBox.get(key);
      if (raw == null) continue;
      final s = (raw as Map)['status'] as int? ?? 0;
      if (s == UploadStatus.FAILED.value ||
          s == UploadStatus.CANCELED.value) {
        keysToDelete.add(key);
      }
    }
    await _uploadBox.deleteAll(keysToDelete);
  }

  Future<List<String>> retryFailedUploadTasks() async {
    final taskIds = <String>[];
    for (final key in _uploadBox.keys) {
      final raw = _uploadBox.get(key);
      if (raw == null) continue;
      final m = Map<String, dynamic>.from(raw);
      if ((m['status'] as int?) == UploadStatus.FAILED.value) {
        m['status'] = UploadStatus.PENDING.value;
        m['error_message'] = null;
        await _uploadBox.put(key, m);
        taskIds.add(key as String);
      }
    }
    return taskIds;
  }

  Future<void> pauseUploadTasks(String? taskId) async {
    if (taskId == null) {
      // Pause all active tasks
      for (final key in _uploadBox.keys) {
        final raw = _uploadBox.get(key);
        if (raw == null) continue;
        final m = Map<String, dynamic>.from(raw);
        final s = m['status'] as int? ?? 0;
        if (s == UploadStatus.UPLOADING.value ||
            s == UploadStatus.PENDING.value ||
            s == UploadStatus.HASHING.value) {
          m['status'] = UploadStatus.PAUSED.value;
          await _uploadBox.put(key, m);
        }
      }
    } else {
      final raw = _uploadBox.get(taskId);
      if (raw == null) return;
      final m = Map<String, dynamic>.from(raw);
      m['status'] = UploadStatus.PAUSED.value;
      await _uploadBox.put(taskId, m);
    }
  }

  Future<List<String>> resumeUploadTasks(String? taskId) async {
    final taskIds = <String>[];
    if (taskId == null) {
      for (final key in _uploadBox.keys) {
        final raw = _uploadBox.get(key);
        if (raw == null) continue;
        final m = Map<String, dynamic>.from(raw);
        if ((m['status'] as int?) == UploadStatus.PAUSED.value) {
          m['status'] = UploadStatus.PENDING.value;
          await _uploadBox.put(key, m);
          taskIds.add(key as String);
        }
      }
    } else {
      final raw = _uploadBox.get(taskId);
      if (raw != null) {
        final m = Map<String, dynamic>.from(raw);
        m['status'] = UploadStatus.PENDING.value;
        await _uploadBox.put(taskId, m);
        taskIds.add(taskId);
      }
    }
    return taskIds;
  }

  Future<void> cancelUploadTask(String? taskId) async {
    if (taskId == null) {
      for (final key in _uploadBox.keys) {
        final raw = _uploadBox.get(key);
        if (raw == null) continue;
        final m = Map<String, dynamic>.from(raw);
        final s = m['status'] as int? ?? 0;
        if (s == UploadStatus.UPLOADING.value ||
            s == UploadStatus.PENDING.value ||
            s == UploadStatus.HASHING.value) {
          m['status'] = UploadStatus.CANCELED.value;
          await _uploadBox.put(key, m);
        }
      }
    } else {
      final raw = _uploadBox.get(taskId);
      if (raw == null) return;
      final m = Map<String, dynamic>.from(raw);
      m['status'] = UploadStatus.CANCELED.value;
      await _uploadBox.put(taskId, m);
    }
  }

  // ==================== Download Tasks ====================

  Future<void> addDownloadTask(DownloadTaskRecord task) async {
    // 去重：检查是否已有相同文件的活跃任务
    for (final key in _downloadBox.keys) {
      final raw = _downloadBox.get(key);
      if (raw == null) continue;
      final m = Map<String, dynamic>.from(raw);
      if (m['file_id'] == task.fileId) {
        final s = m['status'] as int? ?? 0;
        // 跳过活跃状态：pending(0) / downloading(1) / paused(5) / predownload(6)
        if (s == DownloadStatus.pending.index ||
            s == DownloadStatus.downloading.index ||
            s == DownloadStatus.paused.index ||
            s == DownloadStatus.predownload.index) {
          getLogger().info('Download task already exists for file ${task.fileId}, skip');
          return;
        }
      }
    }
    await _downloadBox.put(task.id, task.toJson());
  }

  Future<void> updateDownloadTask(
    String id, {
    DownloadStatus? status,
    String? errorMessage,
    int? downloadedBytes,
    int? speed,
    int? chunkTotal,
  }) async {
    final raw = _downloadBox.get(id);
    if (raw == null) return;
    final existing = Map<String, dynamic>.from(raw);

    if (status != null) {
      existing['status'] = status.index;
      if (status == DownloadStatus.completed) {
        existing['completed_at'] = DateTime.now().toIso8601String();
      }
    }
    if (errorMessage != null) existing['error_message'] = errorMessage;
    if (downloadedBytes != null) existing['downloaded_bytes'] = downloadedBytes;
    if (speed != null) existing['speed'] = speed;
    if (chunkTotal != null) existing['chunk_total'] = chunkTotal;

    await _downloadBox.put(id, existing);
  }

  Future<DownloadTaskRecord?> getDownloadTaskAsync(String id) async {
    final raw = _downloadBox.get(id);
    if (raw == null) return null;
    return DownloadTaskRecord.fromJson(Map<String, dynamic>.from(raw));
  }

  Future<DownloadTaskRecord?> getUncompletedDownloadTaskByFileId(
      String fileId) async {
    for (final raw in _downloadBox.values) {
      final m = Map<String, dynamic>.from(raw);
      if (m['file_id'] == fileId) {
        final s = m['status'] as int? ?? 0;
        if (s == DownloadStatus.pending.index ||
            s == DownloadStatus.downloading.index ||
            s == DownloadStatus.paused.index ||
            s == DownloadStatus.predownload.index) {
          return DownloadTaskRecord.fromJson(m);
        }
      }
    }
    return null;
  }

  Future<List<DownloadTaskRecord>> getAllDownloadTasksAsync(
      {int limit = 100}) async {
    final all = _downloadBox.values
        .map((m) => DownloadTaskRecord.fromJson(Map<String, dynamic>.from(m)))
        .toList()
      ..sort((a, b) => b.createdAt.compareTo(a.createdAt));
    return all.take(limit).toList();
  }

  Future<List<DownloadTaskRecord>> getDownloadTasksByStatus(
    DownloadStatus status, {
    int limit = 100,
    int offset = 0,
  }) async {
    final filtered = _downloadBox.values
        .map((m) => DownloadTaskRecord.fromJson(Map<String, dynamic>.from(m)))
        .where((t) => t.status == status)
        .toList()
      ..sort((a, b) => b.createdAt.compareTo(a.createdAt));
    if (offset > 0) filtered.removeRange(0, offset.clamp(0, filtered.length));
    return filtered.take(limit).toList();
  }

  Future<List<DownloadTaskRecord>> getPendingDownloadTasksAsync() async {
    return _downloadBox.values
        .map((m) => DownloadTaskRecord.fromJson(Map<String, dynamic>.from(m)))
        .where((t) => t.status == DownloadStatus.pending)
        .toList()
      ..sort((a, b) => a.createdAt.compareTo(b.createdAt));
  }

  Future<List<DownloadTaskRecord>> getPausedDownloadTasksAsync() async {
    return _downloadBox.values
        .map((m) => DownloadTaskRecord.fromJson(Map<String, dynamic>.from(m)))
        .where((t) => t.status == DownloadStatus.paused)
        .toList()
      ..sort((a, b) => a.createdAt.compareTo(b.createdAt));
  }

  Future<DownloadTaskRecord?> getNextPendingDownloadTaskAsync() async {
    DownloadTaskRecord? earliest;
    for (final raw in _downloadBox.values) {
      final t = DownloadTaskRecord.fromJson(Map<String, dynamic>.from(raw));
      if (t.status == DownloadStatus.pending) {
        if (earliest == null || t.createdAt.isBefore(earliest.createdAt)) {
          earliest = t;
        }
      }
    }
    return earliest;
  }

  Future<void> deleteDownloadTask(String? id) async {
    if (id == null) {
      await _downloadBox.clear();
    } else {
      await _downloadBox.delete(id);
    }
  }

  Future<void> clearDownloadCompleted() async {
    final keysToDelete = <dynamic>[];
    for (final key in _downloadBox.keys) {
      final raw = _downloadBox.get(key);
      if (raw == null) continue;
      final s = (raw as Map)['status'] as int? ?? 0;
      if (s == DownloadStatus.completed.index) {
        keysToDelete.add(key);
      }
    }
    await _downloadBox.deleteAll(keysToDelete);
  }

  Future<void> pauseDownloadTasks(String? taskId) async {
    if (taskId == null) {
      for (final key in _downloadBox.keys) {
        final raw = _downloadBox.get(key);
        if (raw == null) continue;
        final m = Map<String, dynamic>.from(raw);
        final s = m['status'] as int? ?? 0;
        if (s == DownloadStatus.downloading.index ||
            s == DownloadStatus.pending.index) {
          m['status'] = DownloadStatus.paused.index;
          await _downloadBox.put(key, m);
        }
      }
    } else {
      final raw = _downloadBox.get(taskId);
      if (raw == null) return;
      final m = Map<String, dynamic>.from(raw);
      m['status'] = DownloadStatus.paused.index;
      await _downloadBox.put(taskId, m);
    }
  }

  Future<List<String>> resumeDownloadTasks(String? taskId) async {
    final taskIds = <String>[];
    if (taskId == null) {
      for (final key in _downloadBox.keys) {
        final raw = _downloadBox.get(key);
        if (raw == null) continue;
        final m = Map<String, dynamic>.from(raw);
        if ((m['status'] as int?) == DownloadStatus.paused.index) {
          m['status'] = DownloadStatus.pending.index;
          await _downloadBox.put(key, m);
          taskIds.add(key as String);
        }
      }
    } else {
      final raw = _downloadBox.get(taskId);
      if (raw != null) {
        final m = Map<String, dynamic>.from(raw);
        m['status'] = DownloadStatus.pending.index;
        await _downloadBox.put(taskId, m);
        taskIds.add(taskId);
      }
    }
    return taskIds;
  }

  Future<void> cancelDownloadTask(String? taskId) async {
    if (taskId == null) {
      for (final key in _downloadBox.keys) {
        final raw = _downloadBox.get(key);
        if (raw == null) continue;
        final m = Map<String, dynamic>.from(raw);
        final s = m['status'] as int? ?? 0;
        if (s == DownloadStatus.downloading.index ||
            s == DownloadStatus.pending.index) {
          m['status'] = DownloadStatus.canceled.index;
          await _downloadBox.put(key, m);
        }
      }
    } else {
      final raw = _downloadBox.get(taskId);
      if (raw == null) return;
      final m = Map<String, dynamic>.from(raw);
      m['status'] = DownloadStatus.canceled.index;
      await _downloadBox.put(taskId, m);
    }
  }
}
