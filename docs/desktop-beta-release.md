# Semantix Windows Beta 发布说明

首个桌面 Beta 仅支持 Windows x64 和中文界面。发布资产包括 NSIS 安装包、便携 ZIP 与 `SHA256SUMS.txt`。

安装包暂未进行代码签名，因此 Windows SmartScreen 可能显示“Windows 已保护你的电脑”。从 GitHub Release 下载后，可核对同一 Release 中的 SHA-256；确认发布者与哈希后，选择“更多信息”继续运行。

发布前只走一遍主路径：干净 Windows 安装、选择项目、配置 Provider、完成一次包含工具/审批/Diff 的任务、重启恢复会话，并确认没有演示数据、密钥泄露或未完成入口。

在仓库根目录运行：

```powershell
.\scripts\build-windows-release.ps1 -Version 0.1.0
```
