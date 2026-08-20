# pfm
flutter 项目框架，使用此框架可以快速搭建启动一个flutter项目。

## 引入的核心包说明
- flutter_bloc：强大的状态管理包
- flutter_form_builder & form_builder_validators：表单验证包
- flutter_secure_storage：数据加密本地存储包
- hive_ce_flutter：数据持久化包，同时管理主题和语言切换状态
- intl：官方国际化包
- talker_flutter：日志包，日志分级记录（verbose~error），内存 + 终端输出，并通过自定义 Observer 实时持久化到本地日志文件（10MB×3 轮转，含每日文件），支持崩溃/启动异常排查
- flex_color_scheme：主题包，提供丰富的自带主题
- get_it：全局实例管理
- go_router：路由管理包
- webview_flutter：对接casdoor，可以在app内部展示casdoor的登录页面
- protobuf：protobuf序列化包，与后端共用数据，接口对接的底层支持

# 特性
- 支持国际化

- 支持主题切换

- 集成casdoor登录流程

- 持久化日志：Flutter 业务日志（talker）+ Android 原生日志（Kotlin 写入 filesDir/pfm_logs），启动时序逐阶段打点，未捕获异常自动记录；设置页提供「查看日志」入口，支持关键词搜索、级别/来源过滤与清空，亦可直接导出日志文件

