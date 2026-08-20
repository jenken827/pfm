package com.shifangyuan.pfm

import android.os.Bundle
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.engine.renderer.FlutterUiDisplayListener

/**
 * 冷启动闪屏卡顿排查：记录 Activity 创建、引擎配置、首个 Flutter 帧渲染的
 * 时序（首个 Flutter 帧显示 = 原生闪屏消失）。
 */
class MainActivity : FlutterActivity() {

    @Volatile
    private var firstFrameLogged = false

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
        NativeLog.info("configureFlutterEngine end (elapsed=${NativeLog.elapsedMs()}ms)")
    }

    override fun onDestroy() {
        NativeLog.info("MainActivity.onDestroy (elapsed=${NativeLog.elapsedMs()}ms)")
        super.onDestroy()
    }
}