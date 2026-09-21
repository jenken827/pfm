package com.shifangyuan.pfm

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.documentfile.provider.DocumentFile
import io.flutter.embedding.engine.plugins.activity.ActivityAware
import io.flutter.embedding.engine.plugins.activity.ActivityPluginBinding
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugin.common.PluginRegistry
import java.io.File

/**
 * 通话录音访问通道（SAF 授权目录 + 扫描 + 导出到私有目录）。
 *
 * ## 为什么必须走 SAF，而不能用路径或 MediaStore
 *
 * 实测（华为 STG-AL00 / Android 12，应用 UID 10263）：
 * ```
 * drwxrws--- 3 10132:1023(media_rw)  /storage/emulated/0/Sounds/CallRecord
 * -rwxrwx--- 1 10132:1023(media_rw)  .../xxx.amr
 * ```
 * 目录属主是录音应用、属组 media_rw，而本应用 Groups=`3003 9997 20263 50263`
 * **不含 1023**，因此：
 * - **路径访问被系统拒绝**（`Directory.list()` / `File.read()` 全废）
 * - **MediaStore 也查不到**：实测应用视角 `files` 表 585 行、其中 CallRecord
 *   **0** 行；`audio` 表 0 行；按音频 MIME 过滤也是 0 行。分区存储会对应用隐藏
 *   "非自己拥有"的文件。注意 `adb shell` 能查到 438 行是因为 shell 是系统 UID，
 *   **不能据此认为应用也能读**（这个坑排查时踩过）。
 *
 * 结论：任何"路径 / MediaStore"方案都拿不到这些文件。唯一无需敏感权限的正规途径
 * 是 **SAF**：用户经 `ACTION_OPEN_DOCUMENT_TREE` 手动授权该目录一次，之后应用可
 * 长期（takePersistableUriPermission）经 content:// 读取。
 *
 * ## 方法
 * - pickDirectory: 拉起系统目录选择器，返回授权后的 tree uri 并持久化
 * - hasPersistedPermission: 授权是否仍有效（可能被撤销/目录被删）
 * - scanRecordings: 遍历已授权目录本层文件，返回 JSON 数组
 * - exportRecording: 复制指定文件到私有目录（上传需按路径读），返回副本路径
 * - cleanupExports: 清理私有副本
 */
class CallRecordingChannel(private val context: Context) :
    MethodChannel.MethodCallHandler,
    PluginRegistry.ActivityResultListener {

    companion object {
        private const val REQ_PICK_DIR = 0x9201
        private const val PREF = "call_recording"
        private const val PREF_TREE_URI = "tree_uri"
        private const val EXPORT_DIR = "call_recordings"
    }

    private var pendingResult: MethodChannel.Result? = null
    private var activity: Activity? = null

    /** 录音扩展名白名单（与 Dart 侧 kCallRecordingExtensions 保持一致） */
    private val audioExts = setOf("amr", "m4a", "aac", "mp3", "3gp", "ogg", "wav")

    fun attachActivity(a: Activity?) {
        activity = a
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "pickDirectory" -> pickDirectory(result)
            "hasPersistedPermission" -> result.success(hasPersistedPermission())
            "scanRecordings" -> scanRecordings(result)
            "exportRecording" -> exportRecording(call, result)
            "listExportedCopies" -> listExportedCopies(result)
            "cleanupExports" -> cleanupExports(call, result)
            else -> result.notImplemented()
        }
    }

    // ---------------- SAF 授权 ----------------

    private fun pickDirectory(result: MethodChannel.Result) {
        val act = activity
        if (act == null) {
            result.error("NO_ACTIVITY", "无法获取 Activity，无法打开目录选择器", null)
            return
        }
        if (pendingResult != null) {
            result.error("BUSY", "已有目录选择在进行中", null)
            return
        }
        pendingResult = result
        NativeLog.info("pickDirectory: launching OPEN_DOCUMENT_TREE")
        val intent = Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).apply {
            addFlags(
                Intent.FLAG_GRANT_READ_URI_PERMISSION or
                    Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION,
            )
        }
        try {
            act.startActivityForResult(intent, REQ_PICK_DIR)
        } catch (e: Exception) {
            pendingResult = null
            result.error("PICK_FAILED", e.message, null)
        }
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?): Boolean {
        if (requestCode != REQ_PICK_DIR) return false
        val res = pendingResult ?: return true
        pendingResult = null

        if (resultCode != Activity.RESULT_OK || data?.data == null) {
            // 用户取消：返回 null，Dart 侧保持原配置不变
            NativeLog.info("pickDirectory: cancelled by user")
            res.success(null)
            return true
        }
        val uri = data.data!!
        try {
            // 必须持久化，否则重启后授权失效
            context.contentResolver.takePersistableUriPermission(
                uri,
                Intent.FLAG_GRANT_READ_URI_PERMISSION,
            )
            context.getSharedPreferences(PREF, Context.MODE_PRIVATE)
                .edit().putString(PREF_TREE_URI, uri.toString()).apply()
            NativeLog.info("pickDirectory: granted $uri")
            res.success(uri.toString())
        } catch (e: Exception) {
            NativeLog.info("pickDirectory FAILED err=${e.message}")
            res.error("TAKE_PERMISSION_FAILED", e.message, null)
        }
        return true
    }

    private fun savedTreeUri(): String? =
        context.getSharedPreferences(PREF, Context.MODE_PRIVATE)
            .getString(PREF_TREE_URI, null)

    /**
     * 授权是否仍有效。
     *
     * 可能被用户在设置里撤销、或目录被删除；此时必须重新引导授权，
     * 否则扫描只会静默返回空、用户无从判断原因。
     */
    private fun hasPersistedPermission(): Boolean {
        val u = savedTreeUri() ?: return false
        val uri = Uri.parse(u)
        val ok = context.contentResolver.persistedUriPermissions.any {
            it.uri == uri && it.isReadPermission
        }
        if (!ok) return false
        return DocumentFile.fromTreeUri(context, uri)?.exists() == true
    }

    // ---------------- 扫描 ----------------

    private fun scanRecordings(result: MethodChannel.Result) {
        val tree = treeDocument()
        if (tree == null) {
            NativeLog.info("scanRecordings: no valid tree permission")
            result.success(emptyList<String>())
            return
        }
        val out = mutableListOf<String>()
        try {
            val children = tree.listFiles()
            NativeLog.info("scanRecordings: children=${children.size}")
            for (f in children) {
                if (!f.isFile) continue
                val name = f.name ?: continue
                val dot = name.lastIndexOf('.')
                if (dot <= 0) continue
                if (name.substring(dot + 1).lowercase() !in audioExts) continue
                out.add(
                    buildJson(
                        uri = f.uri.toString(),
                        name = name,
                        size = f.length(),
                        modifiedMs = f.lastModified(),
                    ),
                )
            }
            NativeLog.info("scanRecordings: matched=${out.size}")
            result.success(out)
        } catch (e: Exception) {
            NativeLog.info("scanRecordings FAILED err=${e.message}")
            result.error("SCAN_RECORDINGS_FAILED", e.message, null)
        }
    }

    private fun treeDocument(): DocumentFile? {
        if (!hasPersistedPermission()) return null
        return DocumentFile.fromTreeUri(context, Uri.parse(savedTreeUri()!!))
    }

    // ---------------- 导出到私有目录 ----------------

    /**
     * 复制 SAF 里的录音到应用私有目录，返回副本信息。
     *
     * 必须复制：上传流程按文件路径读内容（io.File(filepath)），
     * content:// 无法当路径用；私有副本路径可用，上传后由 cleanupExports 清理。
     */
    private fun exportRecording(call: MethodCall, result: MethodChannel.Result) {
        val uriStr = call.argument<String>("uri")
        val name = call.argument<String>("name")
        if (uriStr.isNullOrBlank() || name.isNullOrBlank()) {
            result.success(null)
            return
        }
        val targetDir = File(context.filesDir, EXPORT_DIR)
        if (!targetDir.exists() && !targetDir.mkdirs()) {
            result.error("MKDIR_FAILED", "无法创建导出目录", null)
            return
        }
        try {
            val target = File(targetDir, name)
            val stream = context.contentResolver.openInputStream(Uri.parse(uriStr))
            if (stream == null) {
                result.success(null)
                return
            }
            stream.use { input ->
                target.outputStream().use { output -> input.copyTo(output) }
            }
            result.success(
                buildJson(
                    uri = target.absolutePath,
                    name = target.name,
                    size = target.length(),
                    modifiedMs = target.lastModified(),
                ),
            )
        } catch (e: Exception) {
            NativeLog.info("exportRecording FAILED name=$name err=${e.message}")
            result.error("EXPORT_FAILED", e.message, null)
        }
    }

    /**
     * 列出私有目录里已有的副本，返回 [{name, size, dir}]。
     *
     * 供 Dart 侧复用副本用：只要副本还在且大小匹配，就不必重新从
     * SAF 复制一遍。这是"每次备份都重拷全部文件导致卡顿"的对策。
     * dir 字段回传目录绝对路径，避免 Dart 侧硬编码应用私有路径。
     */
    private fun listExportedCopies(result: MethodChannel.Result) {
        val dir = File(context.filesDir, EXPORT_DIR)
        if (!dir.exists()) {
            result.success(emptyList<String>())
            return
        }
        val out = mutableListOf<String>()
        dir.listFiles()?.forEach { f ->
            if (!f.isFile) return@forEach
            out.add(
                buildString {
                    append("{\"name\":").append(escape(f.name))
                    append(",\"size\":").append(f.length())
                    append(",\"dir\":").append(escape(dir.absolutePath))
                    append('}')
                },
            )
        }
        NativeLog.info("listExportedCopies: ${out.size}")
        result.success(out)
    }

    /**
     * 删除私有副本。
     *
     * 入参 names（可选）：只删这些文件名；不传则清空整个目录。
     * **调用方应始终传 names**（只删已上传完成的），因为副本现在会被
     * 复用来跳过重复复制，全量清空会让下次备份重新复制全部文件。
     */
    private fun cleanupExports(call: MethodCall, result: MethodChannel.Result) {
        val dir = File(context.filesDir, EXPORT_DIR)
        if (!dir.exists()) {
            result.success(0)
            return
        }
        val names = call.argument<List<String>>("names")
        var removed = 0
        if (names.isNullOrEmpty()) {
            dir.listFiles()?.forEach { if (it.isFile && it.delete()) removed++ }
        } else {
            for (n in names) {
                val f = File(dir, n)
                if (f.isFile && f.delete()) removed++
            }
        }
        NativeLog.info("cleanupExports: removed=$removed")
        result.success(removed)
    }

    // ---------------- 工具 ----------------

    private fun buildJson(uri: String, name: String, size: Long, modifiedMs: Long) =
        buildString {
            append("{\"uri\":").append(escape(uri))
            append(",\"name\":").append(escape(name))
            append(",\"size\":").append(size)
            append(",\"modified\":").append(modifiedMs)
            append('}')
        }

    private fun escape(s: String): String {
        val sb = StringBuilder(s.length + 16)
        sb.append('"')
        for (ch in s) {
            when (ch) {
                '"' -> sb.append("\\\"")
                '\\' -> sb.append("\\\\")
                '\n' -> sb.append("\\n")
                '\r' -> sb.append("\\r")
                '\t' -> sb.append("\\t")
                else -> if (ch.code < 0x20) {
                    sb.append(String.format("\\u%04x", ch.code))
                } else {
                    sb.append(ch)
                }
            }
        }
        sb.append('"')
        return sb.toString()
    }
}
