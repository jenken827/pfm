package com.shifangyuan.pfm

import android.content.Context
import android.os.SystemClock
import android.util.Log
import java.io.File
import java.io.PrintWriter
import java.io.StringWriter
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * 原生日志：覆盖 Dart 层无法感知的启动早期阶段（进程启动、Activity 创建、
 * Flutter 引擎配置、首帧渲染）与原生未捕获异常。
 *
 * 日志写入 filesDir/pfm_logs/pfm_native_yyyyMMdd.log（应用私有目录，无需权限），
 * 保留最近 [MAX_FILES] 个文件，与 Dart 侧日志互为补充。
 */
object NativeLog {
    private const val TAG = "PfmNative"
    private const val MAX_FILES = 3
    private const val FILE_PREFIX = "pfm_native_"

    @Volatile
    private var logDir: File? = null
    private var processStartElapsed = 0L

    private val dateFormat = SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS", Locale.US)

    /** 必须在 Application.onCreate 中最先调用 */
    fun init(context: Context) {
        processStartElapsed = SystemClock.elapsedRealtime()
        logDir = File(context.filesDir, "pfm_logs").apply { mkdirs() }
        cleanupOldFiles()
        info("process started (pid=${android.os.Process.myPid()})")
    }

    /** 距进程启动的毫秒数（startup 时序分析用） */
    fun elapsedMs(): Long = SystemClock.elapsedRealtime() - processStartElapsed

    fun info(msg: String) = write(Log.INFO, msg)
    fun warn(msg: String) = write(Log.WARN, msg)
    fun error(msg: String, t: Throwable? = null) =
        write(Log.ERROR, if (t != null) "$msg\n${stackTraceOf(t)}" else msg)

    @Synchronized
    private fun write(priority: Int, msg: String) {
        // Logcat 同步输出，便于 adb logcat 排查
        Log.println(priority, TAG, msg.replace('\n', ' '))
        val dir = logDir ?: return
        try {
            val line = "${dateFormat.format(Date())} [${levelChar(priority)}] $msg"
            val file = File(dir, "$FILE_PREFIX${today()}.log")
            file.appendText("$line\n")
        } catch (e: Exception) {
            // 日志丢失不应影响应用
            Log.w(TAG, "write native log failed: ${e.message}")
        }
    }

    private fun cleanupOldFiles() {
        val dir = logDir ?: return
        val files = dir.listFiles { f -> f.name.startsWith(FILE_PREFIX) && f.name.endsWith(".log") }
            ?.sortedBy { it.lastModified() }
            ?: return
        var toDelete = files.size - MAX_FILES
        for (f in files) {
            if (toDelete <= 0) break
            f.delete()
            toDelete--
        }
    }

    private fun today(): String =
        SimpleDateFormat("yyyyMMdd", Locale.US).format(Date())

    private fun levelChar(priority: Int): Char = when (priority) {
        Log.ERROR -> 'E'
        Log.WARN -> 'W'
        Log.DEBUG -> 'D'
        Log.VERBOSE -> 'V'
        else -> 'I'
    }

    private fun stackTraceOf(t: Throwable): String {
        val sw = StringWriter()
        t.printStackTrace(PrintWriter(sw))
        return sw.toString()
    }
}