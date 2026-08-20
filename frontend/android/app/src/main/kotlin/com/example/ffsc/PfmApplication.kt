package com.shifangyuan.pfm

import android.app.Application

/**
 * 应用入口：最早记录进程启动时间，并挂载全局未捕获异常处理器，
 * 使原生启动时序与异常可持久化到日志文件（见 [NativeLog]）。
 */
class PfmApplication : Application() {

    override fun onCreate() {
        super.onCreate()
        NativeLog.init(this)
        NativeLog.info("PfmApplication.onCreate")

        val defaultHandler = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, throwable ->
            NativeLog.error(
                "uncaught exception on thread [${thread.name}]",
                throwable,
            )
            // 转交默认处理器（崩溃弹窗/ANR 上报等）
            defaultHandler?.uncaughtException(thread, throwable)
        }
    }
}