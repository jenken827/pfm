package com.shifangyuan.pfm

import android.content.Intent
import android.os.Bundle
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.engine.renderer.FlutterUiDisplayListener
import io.flutter.plugin.common.MethodChannel

/**
 * 冷启动闪屏卡顿排查：记录 Activity 创建、引擎配置、首个 Flutter 帧渲染的
 * 时序（首个 Flutter 帧显示 = 原生闪屏消失）。
 */
class MainActivity : FlutterActivity() {

    @Volatile
    private var firstFrameLogged = false

    /// 通话录音通道：需要 Activity 拉起 SAF 目录选择器，并在 onActivityResult 回调
    private var callRecordingChannel: CallRecordingChannel? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        NativeLog.info("MainActivity.onCreate start (elapsed=${NativeLog.elapsedMs()}ms)")
        super.onCreate(savedInstanceState)
        NativeLog.info("MainActivity.onCreate end (elapsed=${NativeLog.elapsedMs()}ms)")
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        NativeLog.info("configureFlutterEngine start (elapsed=${NativeLog.elapsedMs()}ms)")
        super.configureFlutterEngine(flutterEngine)

        // 首个 Flutter 帧显示 = 原生闪屏消失。onFlutterUiDisplayed 会随
        // 前后台切换多次回调，用标记位只记录首次。
        flutterEngine.renderer.addIsDisplayingFlutterUiListener(
            object : FlutterUiDisplayListener {
                override fun onFlutterUiDisplayed() {
                    if (!firstFrameLogged) {
                        firstFrameLogged = true
                        NativeLog.info(
                            "first frame rendered, splash dismissed (elapsed=${NativeLog.elapsedMs()}ms)",
                        )
                    }
                }

                override fun onFlutterUiNoLongerDisplayed() {}
            },
        )

        // 短信/通话记录备份读取通道（任务9）
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "pfm/device_backup",
        ).setMethodCallHandler(DeviceBackupChannel(this))

        // 公共下载目录通道：下载完成后把文件从私有目录发布到系统「下载」（其他应用可见）
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "pfm/public_storage",
        ).setMethodCallHandler(PublicStorageChannel(applicationContext))

        // 设备标识通道：备份文件名/文件头要标明来源设备（短信、通话记录按设备独有）
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "pfm/device_info",
        ).setMethodCallHandler(DeviceInfoChannel(this))

        // 通话录音通道：录音目录属 media_rw 组（本应用不在该组），
        // 路径与 MediaStore 均拿不到，必须经 SAF 授权目录后读 content://。
        // SAF 需要 Activity 拉起系统目录选择器，故这里把 this 传进去。
        val recChannel = CallRecordingChannel(applicationContext)
        recChannel.attachActivity(this)
        callRecordingChannel = recChannel
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "pfm/call_recording",
        ).setMethodCallHandler(recChannel)


        NativeLog.info("configureFlutterEngine end (elapsed=${NativeLog.elapsedMs()}ms)")
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        // SAF 目录选择结果先给通话录音通道处理；未消费则交回父类
        if (callRecordingChannel?.onActivityResult(requestCode, resultCode, data) == true) {
            return
        }
        super.onActivityResult(requestCode, resultCode, data)
    }

    override fun onDestroy() {
        NativeLog.info("MainActivity.onDestroy (elapsed=${NativeLog.elapsedMs()}ms)")
        callRecordingChannel?.attachActivity(null)
        callRecordingChannel = null
        super.onDestroy()
    }
}