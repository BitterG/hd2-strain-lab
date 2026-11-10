# HD2 Strain Lab · 绝地潜兵 2 星球变种工坊 (MyGo GPU)

基于 [egoist/mygo](https://github.com/egoist/mygo) 原生 GPU 渲染框架（`package ui`，纯 Go 无 WebView/无 Python 依赖）开发的《绝地潜兵 2》星球变种与代理下发桌面工具。

## 已实现功能（第一阶段）

1. **交互式银河星图与星球选择**
   - 2.5D 透视倾角（42° 俯视，参考 `galaxy-map-v2.html`）与 2D 平面视图一键切换。
   - 24×10 极坐标网格星区合并轮廓与敌方控制区着色，悬停星球显示左上角星区战况卡。
   - 补给线（跨阵营我方蓝→敌方色渐变）、防守入侵红虚线箭头、阴霾（The Gloom）与 TCS+ 软边雾气、在线玩家热力分级节点半径。
   - 滚轮缩放（锚定光标）、左/中/右键拖动平移、单击选球、`Ctrl+单击` 多选批量配置、双击聚焦、支持直接从星图拾取防守战役进攻来源。
   - 左侧星球搜索与阵营筛选，支持一键拉取官方实时战况（`Status`）并实时同步代理观测到的战况。

2. **配置下发与纯 Go 本机代理修改**
   - 内置 315 项星系效果与变种目录（支持中英文/ID 搜索与分组筛选），支持自动补全 `EFFECT_PAIRS`（Tag + 图标成对效果）。
   - 支持配置星球归属、开启/强制开放战役、战役类型（常规解放 / 城市特殊 / 防守战役）、作战槽数量、分区开放与初始化原生分区。
   - 纯 Go TLS 选择性拦截代理：仅解密 `api.live.prod.thehelldiversgame.com:443`，其余连接与登录/反作弊流量直连透传不解密；支持配置上游 HTTP 代理兼容加速器。
   - 自动生成本机 CA、支持安装/移除到 Windows `CurrentUser\ROOT`（无需管理员权限修改 `hosts`），内置 loopback CRL 吊销服务避免 Schannel 校验失败。
   - 支持单 EXE `--steam-bridge %command%` Steam 启动桥接与方案热重载下发。

## 构建与运行

需要 Go 1.27.3+（本项目默认使用 `D:\go1.27.3.windows-amd64\go`）：

```powershell
.\dev.ps1     # 开发运行
.\build.ps1   # 构建单文件 hd2-strain-lab.exe
```

## 参考与致谢

- [custom-planets-variants](https://github.com/127MU/custom-planets-variants)
- [Helldivers Wiki (Illuminate)](https://helldivers.wiki.gg/wiki/Illuminate)
- [Helldivers Wiki](https://helldivers.wiki.gg)
- [HD2 中文维基 / 银河地图参考](https://jerry114514.github.io/HD2_Wiki/wiki.html)
- [egoist/mygo](https://github.com/egoist/mygo)
- [Bilibili 相关视频 BV198456AEzn](https://www.bilibili.com/video/BV198456AEzn)
- [Bilibili 作者主页](https://space.bilibili.com/1792463983)
