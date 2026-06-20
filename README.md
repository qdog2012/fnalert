# fnmonitor

fnmonitor 是一个面向飞牛/fnOS 或普通 Linux 主机的 Go 监控服务。它会采集系统指标、读取系统日志、按阈值产生告警，并通过飞牛系统邮件适配器、独立 SMTP 或 Webhook 发送通知。

## 功能

- Web 仪表盘和 JSON API：`/`、`/api/metrics`、`/api/logs`、`/api/alerts`
- 指标采集：CPU、负载、内存、Swap、磁盘空间、inode、磁盘 IO、网卡流量、温度、进程数、Top 内存进程、系统信息
- 日志读取：`journalctl`、`dmesg`、`/var/log/syslog`、`/var/log/messages`、`/var/log/kern.log`、`/var/log/auth.log`
- 告警规则：CPU、负载、内存、Swap、磁盘、inode、温度、进程数、网卡速率、可配置日志级别和异常关键字
- 通知渠道：stdout、飞牛系统邮件适配器、独立 SMTP、通用 Webhook；同一轮采集的多个异常会合并为一次通知

## 快速开始

```bash
go mod tidy
go run ./cmd/fnmonitor -config config.example.yaml
```

打开 `http://127.0.0.1:39187` 查看仪表盘。

## 常用命令

```bash
# 只采集一次并输出 JSON
go run ./cmd/fnmonitor -config config.example.yaml -once

# 发送测试通知
go run ./cmd/fnmonitor -config config.example.yaml -test-notify
```

## 构建飞牛安装包

```bash
./scripts/build-fnos-fpk.sh
```

默认输出：

```bash
dist/fnmonitor_0.1.14_x86.fpk
```

可用环境变量覆盖版本、平台和端口：

```bash
VERSION=0.1.1 PLATFORM=x86 PORT=39187 ./scripts/build-fnos-fpk.sh
VERSION=0.1.1 PLATFORM=arm PORT=39187 ./scripts/build-fnos-fpk.sh
```

安装包以 root 权限运行，以便读取系统日志、`dmesg`、硬件温度和完整进程指标。安装包包含飞牛原生配置表单，可修改 Web 端口、采集间隔、告警阈值、Swap 告警阈值、系统日志过滤级别、SMTP、Webhook 和飞牛邮件适配器。SMTP 支持 STARTTLS（常用 587）、SSL/TLS（常用 465）、不加密模式和发件人昵称。安装后配置文件会放在应用数据目录的 `config.yaml`，升级不会覆盖已有配置。fnOS 原生配置表单不支持单独测试按钮；只有 SMTP、Webhook 或飞牛邮件适配器配置发生变化时，保存后才会发送测试通知。测试失败不会阻止保存，失败原因会写入应用日志和 `last-notification-test.log`，应用 Web 设置页会显示最近一次测试结果。

Swap 默认告警阈值为 `98%`。如果 Linux 长时间占用 Swap 但系统不卡、`vmstat` 的 `si/so` 基本为 0，可以在飞牛配置表单中把“Swap 使用率 %”设为 `0` 来关闭 Swap 告警。

日志通知默认规则：系统日志过滤级别为“警告及以上”，即 `warning`、`error`、`critical` 会触发；也可改为全部日志、错误及以上、严重错误或关闭日志告警。异常关键字匹配按 `error` 级别处理。飞牛配置表单中的“日志告警触发条数”默认为 `1`，设置为 `0` 也可关闭日志触发通知。同类日志通知仍受“通知冷却”限制。

## 飞牛系统邮件集成

fnOS 的公开帮助文档说明了在系统设置中配置 SMTP 授权，但未公开一个稳定的本机“发送邮件 API”。参考：[如何启用邮件通知服务](https://help.fnnas.com/articles/v1/settings/setup-smtp)。因此 `fnos_email` 渠道采用适配器方式：

- `mode: command`：调用 fnOS 本机提供的邮件发送命令或你封装的脚本，邮件正文通过 stdin 或参数传入。fnmonitor 不假设存在固定命令路径。
- `mode: http`：调用本机 HTTP 入口，发送 JSON `{title, body, severity, labels}`。

这样可以复用飞牛系统已有邮件配置，不需要在 fnmonitor 里重复保存 SMTP 密码。若你的 fnOS 版本没有暴露发送入口，可以启用 `notify.channels.smtp` 使用独立 SMTP 配置。

## 部署建议

- 用 root 或具备日志读取权限的用户运行，否则 `journalctl`、`dmesg`、部分 `/var/log/*` 可能读取失败。
- 将 `config.example.yaml` 复制为生产配置，设置通知渠道和阈值。
- 用 systemd 管理服务，并限制配置文件权限，特别是独立 SMTP 密码环境变量。
