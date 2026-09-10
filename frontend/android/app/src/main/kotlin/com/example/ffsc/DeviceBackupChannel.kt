package com.shifangyuan.pfm

import android.content.ContentResolver
import android.content.Context
import android.database.Cursor
import android.provider.CallLog
import android.provider.Telephony
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel

/**
 * 短信/通话记录读取通道（任务9：短信与通话记录备份）。
 *
 * 提供两个方法，均返回 JSON 字符串列表：
 * - readSms: 读取全部短信（content://sms）
 *   字段: id, address, date(ms), body, type(inbox=1/sent=2/...), read(0/1)
 * - readCallLog: 读取全部通话记录（content://call_log/calls）
 *   字段: id, number, date(ms), duration(s), type(INCOMING/OUTGOING/MISSED),
 *         cachedName, isNew
 *
 * 数据量大，直接以 JSON 字符串数组返回（单条消息独立字符串），
 * 避免 Dart 侧大对象序列化开销。
 */
class DeviceBackupChannel(context: Context) : MethodChannel.MethodCallHandler {

    private val resolver: ContentResolver = context.contentResolver

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "readSms" -> readSms(result)
            "readCallLog" -> readCallLog(result)
            else -> result.notImplemented()
        }
    }

    /** 读取全部短信，返回 JSON 字符串数组 */
    private fun readSms(result: MethodChannel.Result) {
        try {
            val list = mutableListOf<String>()
            val uri = Telephony.Sms.CONTENT_URI
            val projection = arrayOf(
                Telephony.Sms._ID,
                Telephony.Sms.ADDRESS,
                Telephony.Sms.DATE,
                Telephony.Sms.BODY,
                Telephony.Sms.TYPE,
                Telephony.Sms.READ,
            )
            val cursor: Cursor? = resolver.query(uri, projection, null, null, "${Telephony.Sms.DATE} ASC")
            cursor?.use { c ->
                val idIdx = c.getColumnIndexOrThrow(Telephony.Sms._ID)
                val addrIdx = c.getColumnIndexOrThrow(Telephony.Sms.ADDRESS)
                val dateIdx = c.getColumnIndexOrThrow(Telephony.Sms.DATE)
                val bodyIdx = c.getColumnIndexOrThrow(Telephony.Sms.BODY)
                val typeIdx = c.getColumnIndexOrThrow(Telephony.Sms.TYPE)
                val readIdx = c.getColumnIndexOrThrow(Telephony.Sms.READ)
                while (c.moveToNext()) {
                    val id = c.getLong(idIdx)
                    val address = c.getString(addrIdx).orEmpty()
                    val date = c.getLong(dateIdx)
                    val body = c.getString(bodyIdx).orEmpty()
                    val type = c.getInt(typeIdx)
                    val read = c.getInt(readIdx)
                    // 逐条 JSON 序列化，body 需转义
                    val json = buildString {
                        append("{\"id\":").append(id)
                        append(",\"address\":").append(escape(address))
                        append(",\"date\":").append(date)
                        append(",\"body\":").append(escape(body))
                        append(",\"type\":").append(type)
                        append(",\"read\":").append(read)
                        append('}')
                    }
                    list.add(json)
                }
            }
            result.success(list)
        } catch (e: Exception) {
            result.error("READ_SMS_FAILED", e.message, null)
        }
    }

    /** 读取全部通话记录，返回 JSON 字符串数组 */
    private fun readCallLog(result: MethodChannel.Result) {
        try {
            val list = mutableListOf<String>()
            val uri = CallLog.Calls.CONTENT_URI
            val projection = arrayOf(
                CallLog.Calls._ID,
                CallLog.Calls.NUMBER,
                CallLog.Calls.DATE,
                CallLog.Calls.DURATION,
                CallLog.Calls.TYPE,
                CallLog.Calls.CACHED_NAME,
                CallLog.Calls.IS_READ,
            )
            val cursor: Cursor? = resolver.query(uri, projection, null, null, "${CallLog.Calls.DATE} ASC")
            cursor?.use { c ->
                val idIdx = c.getColumnIndexOrThrow(CallLog.Calls._ID)
                val numIdx = c.getColumnIndexOrThrow(CallLog.Calls.NUMBER)
                val dateIdx = c.getColumnIndexOrThrow(CallLog.Calls.DATE)
                val durIdx = c.getColumnIndexOrThrow(CallLog.Calls.DURATION)
                val typeIdx = c.getColumnIndexOrThrow(CallLog.Calls.TYPE)
                val nameIdx = c.getColumnIndexOrThrow(CallLog.Calls.CACHED_NAME)
                val readIdx = c.getColumnIndexOrThrow(CallLog.Calls.IS_READ)
                while (c.moveToNext()) {
                    val id = c.getLong(idIdx)
                    val number = c.getString(numIdx).orEmpty()
                    val date = c.getLong(dateIdx)
                    val duration = c.getLong(durIdx)
                    val type = c.getInt(typeIdx)
                    val name = c.getString(nameIdx).orEmpty()
                    val isNew = c.getInt(readIdx)
                    val json = buildString {
                        append("{\"id\":").append(id)
                        append(",\"number\":").append(escape(number))
                        append(",\"date\":").append(date)
                        append(",\"duration\":").append(duration)
                        append(",\"type\":").append(type)
                        append(",\"name\":").append(escape(name))
                        append(",\"isNew\":").append(isNew)
                        append('}')
                    }
                    list.add(json)
                }
            }
            result.success(list)
        } catch (e: Exception) {
            result.error("READ_CALL_LOG_FAILED", e.message, null)
        }
    }

    /** JSON 字符串转义（消息内容/号码可能含引号、反斜杠、控制字符） */
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
                '\b' -> sb.append("\\b")
                '\u000C' -> sb.append("\\f")
                else -> {
                    if (ch.code < 0x20) {
                        sb.append(String.format("\\u%04x", ch.code))
                    } else {
                        sb.append(ch)
                    }
                }
            }
        }
        sb.append('"')
        return sb.toString()
    }
}
