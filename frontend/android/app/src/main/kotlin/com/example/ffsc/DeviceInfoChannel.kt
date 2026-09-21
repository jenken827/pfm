package com.shifangyuan.pfm

import android.content.Context
import android.os.Build
import android.provider.Settings
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel

/**
 * 设备信息读取通道（短信/通话记录备份需要标明来源设备）。
 *
 * 短信、通话记录这类数据**按设备独有**（每台手机的短信/通话记录不会互通），
 * 多设备备份到同一目录会混作一谈，故备份文件名与文件头都要带设备信息。
 * 通讯录因为是跨设备同步的同一份数据，不需要标识。
 *
 * 方法：
 * - getDeviceType: 设备**类型**（人类可读的型号名，如 "华为畅享 60X"）
 * - getDeviceId:   设备**标识**（用户可改的设备名，如 "huaweufhfg"）
 * - getDeviceModel: 原始型号代号（`Build.MODEL`，如 "STG-AL00"，仅作兜底）
 *
 * ## 取值来源（华为 STG-AL00 实测确定）
 *
 * 实测该机：
 * - `Settings.Global.DEVICE_NAME`  = "STG-AL00"    ← 华为把**型号代号**塞在这里，
 *                                                    并非用户改的名字，无区分度
 * - `Settings.Secure.BLUETOOTH_NAME` = "huaweufhfg" ← 设置里「设备名称」真正落盘处
 * - `ro.config.marketing_name`    = "华为畅享 60X"  ← 厂商市场名（人类可读）
 *
 * 因此**类型**与**标识**分开取，各用各的来源，不再互相兜底成同一个值 ——
 * 否则文件名里会出现 "STG-AL00_STG-AL00" 这种重复且无意义的组合。
 */
class DeviceInfoChannel(private val context: Context) : MethodChannel.MethodCallHandler {

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "getDeviceType" -> result.success(deviceType())
            "getDeviceId" -> result.success(deviceId())
            "getDeviceModel" -> result.success(Build.MODEL.orEmpty())
            else -> result.notImplemented()
        }
    }

    /**
     * 设备类型：给人看的型号名，优先厂商市场名，退回厂商-型号。
     *
     * 不含用户可改的部分 —— 用户改设备名不该改变"这是什么型号"。
     */
    private fun deviceType(): String {
        marketingName()?.takeIf { it.isNotBlank() }?.let { return it.trim() }
        return modelLabel()
    }

    /**
     * 设备标识：用户可改的设备名，用于区分**同型号的多台设备**。
     *
     * 优先级：设置里「设备名称」（Secure.BLUETOOTH_NAME）→
     * `Global.DEVICE_NAME`（仅当它不等于型号代号时才算有效名字）→
     * 空串（由调用方决定是否省略该段）。
     *
     * 不退回型号：型号已由 [deviceType] 提供，这里若退回型号会让文件名
     * 出现重复段（如 "华为畅享 60X_STG-AL00"），且同型号设备仍无法区分。
     */
    private fun deviceId(): String {
        bluetoothName()?.takeIf { it.isNotBlank() }?.let { return it.trim() }

        // DEVICE_NAME 只有"不等于型号代号"时才算有效：华为等 ROM 会把型号
        // 塞进该字段，此时它没有任何区分度，不如留空
        globalDeviceName()
            ?.takeIf { it.isNotBlank() && !isJustModel(it.trim()) }
            ?.let { return it.trim() }

        return ""
    }

    /**
     * 设置里「设备名称」真正落盘的字段。
     * 部分 ROM/未开启蓝牙时可能为空，读取异常一律吞掉继续往下找。
     */
    private fun bluetoothName(): String? = try {
        @Suppress("DEPRECATION")
        Settings.Secure.getString(context.contentResolver, "bluetooth_name")
    } catch (e: Exception) {
        null
    }

    /**
     * Settings.Global.DEVICE_NAME。
     *
     * 该 key 在部分 ROM 上未定义（返回 null），也可能因权限抛异常，
     * 一律吞掉退回后续策略，不能让取名字失败阻断整个备份。
     */
    private fun globalDeviceName(): String? = try {
        @Suppress("DEPRECATION")
        Settings.Global.getString(context.contentResolver, "device_name")
    } catch (e: Exception) {
        null
    }

    /**
     * 厂商市场名，如华为的 "华为畅享 60X"、小米的 "Xiaomi 14"。
     *
     * `ro.config.marketing_name` 是华为/部分国产 ROM 用的；
     * `ro.product.marketname` 是 AOSP 后续版本引入的，两者都试。
     */
    private fun marketingName(): String? {
        val keys = listOf(
            "ro.config.marketing_name",
            "ro.product.marketname",
            "ro.product.vendor.marketname",
        )
        for (key in keys) {
            val value = systemProperty(key)
            if (!value.isNullOrBlank()) return value
        }
        return null
    }

    @Suppress("TooGenericExceptionCaught")
    private fun systemProperty(key: String): String? = try {
        val clazz = Class.forName("android.os.SystemProperties")
        val get = clazz.getMethod("get", String::class.java)
        (get.invoke(null, key) as? String)?.takeIf { it.isNotBlank() && it != "unknown" }
    } catch (e: Exception) {
        null
    }

    /** 判断某个名字是否只是型号代号（与 Build.MODEL / Build.DEVICE 相同即无信息量） */
    private fun isJustModel(value: String): Boolean {
        val model = Build.MODEL.orEmpty()
        val device = Build.DEVICE.orEmpty()
        if (value.equals(model, ignoreCase = true)) return true
        if (value.equals(device, ignoreCase = true)) return true
        // 华为 device_name="STG-AL00" 与 model="STG-AL00" 相同；部分 ROM 用下划线异体
        return value.replace('_', '-').equals(model.replace('_', '-'), ignoreCase = true)
    }

    /** 厂商-型号，如 "HUAWEI-STG-AL00" */
    private fun modelLabel(): String {
        val manufacturer = Build.MANUFACTURER.orEmpty().trim()
        val model = Build.MODEL.orEmpty().trim()
        if (manufacturer.isEmpty()) return model
        if (model.isEmpty()) return manufacturer
        // 有些 ROM 的 MODEL 已自带厂商前缀（如 "Xiaomi 14"），避免 "Xiaomi-Xiaomi 14"
        return if (model.startsWith(manufacturer, ignoreCase = true)) {
            model
        } else {
            "$manufacturer-$model"
        }
    }
}
