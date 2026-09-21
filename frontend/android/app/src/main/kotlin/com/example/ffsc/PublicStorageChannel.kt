package com.shifangyuan.pfm

import android.content.ContentValues
import android.content.Intent
import android.content.Context
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.io.FileInputStream
import java.io.OutputStream

/**
 * 公共下载目录通道：把已下载完的文件从应用私有目录「发布」到系统「下载」。
 *
 * 背景：path_provider 的 getDownloadsDirectory() 在 Android 上走的是
 * Context.getExternalFilesDirs(DIRECTORY_DOWNLOADS)，返回应用专属外部目录
 * （/storage/emulated/0/Android/data/<pkg>/files/Download），其他应用与系统
 * 「文件管理器/下载」都访问不到，卸载还会一并删除。
 *
 * 为什么是「下载完再发布」而不是直接下载到公共目录：
 * 分区存储（Scoped Storage）对公共目录的写入是**路径级白名单**，不是普通
 * unix 权限位。没有 MANAGE_EXTERNAL_STORAGE 时，无论 rename / copy+unlink，
 * 只要目标路径在公共 Download/ 下，FUSE 层一律拒绝（EACCES）。唯一合法途径
 * 是经由 MediaStore 让系统组件代写。而 MediaStore 需要 ContentResolver
 * 流式写，与 Dart 侧的多线程 Range 分片 + 断点续传（RandomAccessFile.setPosition）
 * 不兼容 —— 因此下载全程留在私有目录，完成后由本通道整体发布。
 *
 * 好处：零敏感权限（不需要「所有文件访问」，可上架 Play）、断点续传不受影响、
 * 下载中途不会在公共目录留下半截文件。
 *
 * 提供方法：
 * - getPublicDownloadsDir: 公共下载目录绝对路径（仅用于展示/兜底）
 * - getSdkInt: 系统 API 级别（Dart 侧据此决定权限分流）
 * - publishToDownloads(srcPath, fileName): 把私有文件发布到公共「下载」，返回
 *   `{path, uri}`——path 是实际落地的绝对路径（重名时系统会追加 " (1)" 后缀），
 *   uri 是 MediaStore 的 content:// uri（仅 API 29+ 非空），供打开文件使用。
 * - openPublished(uri, mimeType): 用 content:// uri 打开已发布文件
 * - deletePublicFile(target): 删除已发布的公共文件
 *
 * 注意：所有阻塞 IO 必须放在工作线程，否则会卡住主线程渲染。
 */
class PublicStorageChannel(private val context: Context) : MethodChannel.MethodCallHandler {

    companion object {
        /** 公共「下载」目录下的相对路径（MediaStore RELATIVE_PATH） */
        private const val RELATIVE_DOWNLOAD_DIR = "Download/"
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "getPublicDownloadsDir" -> result.success(publicDownloadsDir())
            "getSdkInt" -> result.success(Build.VERSION.SDK_INT)
            "publishToDownloads" -> {
                val srcPath = call.argument<String>("srcPath")
                val fileName = call.argument<String>("fileName")
                if (srcPath.isNullOrEmpty() || fileName.isNullOrEmpty()) {
                    result.error("BAD_ARGS", "srcPath/fileName required", null)
                    return
                }
                // 拷贝是大文件阻塞 IO，必须放到工作线程
                Thread {
                    try {
                        postSuccess(result, publish(srcPath, fileName))
                    } catch (e: Exception) {
                        postError(result, e)
                    }
                }.start()
            }
            "openPublished" -> {
                // 用 content:// 打开已发布文件：绕开 open_file 插件对公共目录的
                // canRead()/MANAGE_EXTERNAL_STORAGE 检查（我们并不持有该权限）。
                val uriString = call.argument<String>("uri")
                val mimeType = call.argument<String>("mimeType")
                if (uriString.isNullOrEmpty()) {
                    result.error("BAD_ARGS", "uri required", null)
                    return
                }
                val ok = try {
                    openPublished(Uri.parse(uriString), mimeType)
                } catch (e: Exception) {
                    false
                }
                result.success(ok)
            }
            "deletePublicFile" -> {
                val target = call.argument<String>("target")
                if (target.isNullOrEmpty()) {
                    result.error("BAD_ARGS", "target required", null)
                    return
                }
                Thread {
                    try {
                        postSuccess(result, deletePub(target))
                    } catch (e: Exception) {
                        postError(result, e)
                    }
                }.start()
            }
            else -> result.notImplemented()
        }
    }

    // ================================================================
    // 发布
    // ================================================================

    /**
     * 把 [srcPath] 发布到公共「下载」目录，文件名为 [fileName]。
     *
     * API 29+ 走 MediaStore（系统代写，无需敏感权限）；
     * API < 29 上公共目录是普通可写目录（WRITE_EXTERNAL_STORAGE 有效），
     * 同挂载点直接 rename，跨挂载点退化为拷贝+删源。
     *
     * 返回实际落地的绝对路径（MediaStore 可能因重名自动改名）。
     * 不删除源文件——源文件的删除由调用方在确认发布成功后执行，保证失败可回滚。
     */
    private fun publish(srcPath: String, fileName: String): Map<String, Any?> {
        val src = File(srcPath)
        if (!src.exists()) throw IllegalArgumentException("source not found: $srcPath")
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            publishViaMediaStore(src, fileName)
        } else {
            publishViaFileRename(src, fileName)
        }
    }

    /** API 29+：MediaStore 插入记录 → openOutputStream 流式拷贝 */
    private fun publishViaMediaStore(src: File, fileName: String): Map<String, Any?> {
        // 始终写入 Downloads 集合：目标位置是公共「下载」（RELATIVE_PATH=Download/）。
        // MediaStore.Images 对应 Pictures/，用它写 Download/ 会得到不一致的记录，
        // 且相册类应用同样能扫描到 Download/ 下的图片，无需换集合。
        val collection = MediaStore.Downloads.EXTERNAL_CONTENT_URI

        val values = ContentValues().apply {
            put(MediaStore.MediaColumns.DISPLAY_NAME, fileName)
            put(MediaStore.MediaColumns.RELATIVE_PATH, RELATIVE_DOWNLOAD_DIR)
            // IS_PENDING=1：写入期间其他应用看不到半截文件，写完置 0 才可见
            put(MediaStore.MediaColumns.IS_PENDING, 1)
        }

        val uri: Uri = context.contentResolver.insert(collection, values)
            ?: throw IllegalStateException("MediaStore insert failed for $fileName")

        try {
            val out: OutputStream = context.contentResolver.openOutputStream(uri)
                ?: throw IllegalStateException("openOutputStream failed for $uri")
            FileInputStream(src).use { input ->
                out.use { output -> input.copyTo(output, DEFAULT_BUFFER_SIZE) }
            }
            // IS_PENDING=0 后才对其他应用可见
            val done = ContentValues().apply {
                put(MediaStore.MediaColumns.IS_PENDING, 0)
            }
            context.contentResolver.update(uri, done, null, null)
        } catch (e: Exception) {
            // 写入失败要清掉这条半截记录，否则公共目录里会留下损坏条目
            runCatching { context.contentResolver.delete(uri, null, null) }
            throw e
        }

        // path 供展示/去重，uri 供打开（content:// 不需要 MANAGE_EXTERNAL_STORAGE，
        // 因为读权限由 MediaStore 授予本应用该条记录）
        val path = queryAbsolutePath(uri) ?: "${publicDownloadsDir()}/$fileName"
        return mapOf("path" to path, "uri" to uri.toString())
    }

    /**
     * 查询 MediaStore 记录的实际落地路径（可能因重名被系统改名）。
     *
     * DATA 列自 API 30 起被弃用、部分设备直接屏蔽，查询它会抛
     * IllegalArgumentException，因此仅作可选项并单独 try/catch；
     * 拿不到时退化为「公共下载目录 + 实际 DISPLAY_NAME」，两者等价。
     */
    private fun queryAbsolutePath(uri: Uri): String? {
        val projection = arrayOf(
            MediaStore.MediaColumns.DATA,
            MediaStore.MediaColumns.DISPLAY_NAME,
        )
        val cursor = try {
            context.contentResolver.query(uri, projection, null, null, null)
        } catch (e: Exception) {
            // DATA 列不可查询（API 30+ 常见），退化为按 DISPLAY_NAME 拼装
            queryByDisplayName(uri)
        }
        cursor?.use {
            if (it.moveToFirst()) {
                val dataIdx = it.getColumnIndex(MediaStore.MediaColumns.DATA)
                if (dataIdx >= 0) {
                    val path = it.getString(dataIdx)
                    if (!path.isNullOrEmpty()) return path
                }
                val nameIdx = it.getColumnIndex(MediaStore.MediaColumns.DISPLAY_NAME)
                if (nameIdx >= 0) {
                    val name = it.getString(nameIdx)
                    if (!name.isNullOrEmpty()) return "${publicDownloadsDir()}/$name"
                }
            }
        }
        return null
    }

    /** 仅查询 DISPLAY_NAME（不触碰被屏蔽的 DATA 列） */
    private fun queryByDisplayName(uri: Uri): android.database.Cursor? = try {
        context.contentResolver.query(
            uri,
            arrayOf(MediaStore.MediaColumns.DISPLAY_NAME),
            null,
            null,
            null,
        )
    } catch (e: Exception) {
        null
    }

    /** API < 29：公共目录普通可写，优先 rename，跨挂载点退化为拷贝+删源 */
    private fun publishViaFileRename(src: File, fileName: String): Map<String, Any?> {
        val dir = File(publicDownloadsDir())
        if (!dir.exists() && !dir.mkdirs()) {
            throw IllegalStateException("cannot create dir: ${dir.absolutePath}")
        }
        var dest = File(dir, fileName)
        // 同目录重名时追加后缀，避免覆盖用户已有文件
        if (dest.exists()) {
            dest = uniqueFile(dir, fileName)
        }
        if (!src.renameTo(dest)) {
            // rename 失败（多为跨挂载点 EXDEV）：拷贝后删源
            FileInputStream(src).use { input ->
                dest.outputStream().use { output -> input.copyTo(output, DEFAULT_BUFFER_SIZE) }
            }
            if (!dest.exists() || dest.length() != src.length()) {
                throw IllegalStateException("copy verify failed: ${dest.absolutePath}")
            }
            src.delete()
        }
        // API < 29 无需 uri：该级别下公共目录文件可读（WRITE_EXTERNAL_STORAGE 有效），
        // 直接按路径交给 open_file 即可，故 uri 留空由 Dart 侧走旧路径
        return mapOf("path" to dest.absolutePath, "uri" to null)
    }

    /**
     * 用 content:// uri 打开已发布文件。
     *
     * 为什么不交给 open_file 插件：其 Android 端对文件路径会先做
     * `!file.canRead()` 判断，公共目录下会进而要求 MANAGE_EXTERNAL_STORAGE
     * （open_file_android OpenFilePlugin.java:91-110），而我们刻意不申请该权限，
     * 导致「打开文件」对已发布文件必然失败。这里直接构造 ACTION_VIEW + uri，
     * 读权限由 MediaStore 随 uri 授予（FLAG_GRANT_READ_URI_PERMISSION），
     * 无需任何敏感权限。
     */
    private fun openPublished(uri: Uri, mimeType: String?): Boolean {
        val resolvedType = mimeType
            ?.takeIf { it.isNotEmpty() }
            ?: context.contentResolver.getType(uri)
            ?: "*/*"
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, resolvedType)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        val handlers = context.packageManager.queryIntentActivities(intent, 0)
        if (handlers.isEmpty()) return false
        // 逐个授予读权限：不依赖 FLAG 在某些 ROM 上的传播行为
        for (h in handlers) {
            runCatching {
                context.grantUriPermission(
                    h.activityInfo.packageName,
                    uri,
                    Intent.FLAG_GRANT_READ_URI_PERMISSION,
                )
            }
        }
        return try {
            context.startActivity(intent)
            true
        } catch (e: Exception) {
            false
        }
    }

    /** 生成不冲突的文件名：a.txt → a (1).txt → a (2).txt */
    private fun uniqueFile(dir: File, fileName: String): File {
        val dot = fileName.lastIndexOf('.')
        val base = if (dot > 0) fileName.substring(0, dot) else fileName
        val ext = if (dot > 0) fileName.substring(dot) else ""
        var i = 1
        while (true) {
            val candidate = File(dir, "$base ($i)$ext")
            if (!candidate.exists()) return candidate
            i++
        }
    }

    // ================================================================
    // 删除
    // ================================================================

    /**
     * 删除已发布的公共文件。
     *
     * [target] 是 publishToDownloads 返回的绝对路径。API 29+ 需要先按路径反查
     * MediaStore 的 uri 再删（直接 File.delete 会留下 MediaStore 孤儿记录）；
     * 该文件若不属于本应用插入的记录，系统会抛异常，调用方应容忍。
     */
    private fun deletePub(target: String): Boolean {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            val uri = findMediaStoreUri(target)
            if (uri != null) {
                return context.contentResolver.delete(uri, null, null) > 0
            }
        }
        return File(target).delete()
    }

    /** 按绝对路径反查 MediaStore 记录 uri（含已 pending 的） */
    private fun findMediaStoreUri(target: String): Uri? {
        val collection = MediaStore.Downloads.EXTERNAL_CONTENT_URI
        val projection = arrayOf(MediaStore.MediaColumns._ID, MediaStore.MediaColumns.DATA)
        context.contentResolver.query(
            collection,
            projection,
            "${MediaStore.MediaColumns.DATA} = ?",
            arrayOf(target),
            null,
        )?.use { cursor ->
            if (cursor.moveToFirst()) {
                val idIdx = cursor.getColumnIndex(MediaStore.MediaColumns._ID)
                if (idIdx >= 0) {
                    return Uri.withAppendedPath(collection, cursor.getLong(idIdx).toString())
                }
            }
        }
        return null
    }

    // ================================================================
    // 工具
    // ================================================================

    /**
     * 公共下载目录绝对路径。
     *
     * getExternalStoragePublicDirectory 自 API 29 起标记 deprecated（推荐 MediaStore），
     * 此处仅用于拼装展示路径与 API<29 的直写，且能正确处理多用户/工作资料
     * （/storage/emulated/<userId>），不要硬编码 /emulated/0。
     */
    @Suppress("DEPRECATION")
    private fun publicDownloadsDir(): String =
        Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS).absolutePath

    private fun postSuccess(result: MethodChannel.Result, value: Any?) {
        (context as? android.app.Activity)?.runOnUiThread { result.success(value) }
            ?: android.os.Handler(context.mainLooper).post { result.success(value) }
    }

    private fun postError(result: MethodChannel.Result, e: Exception) {
        val runnable = { result.error("PUBLISH_FAILED", e.message, null) }
        (context as? android.app.Activity)?.runOnUiThread(runnable)
            ?: android.os.Handler(context.mainLooper).post(runnable)
    }
}
