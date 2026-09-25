<div align="center">

# GlycCat

_✨ 基于 [Satori](https://satori.chat/zh-CN/) 协议的 QQ 官方机器人 API GoLang 实现 ✨_

</div>

## 引用

本项目使用以下项目提供平台通信和协议实现：

- [`WindowsSov8forUs/botgo-plus`](https://github.com/WindowsSov8forUs/botgo-plus)
- [`satori-protocol-go/satori-go`](https://github.com/satori-protocol-go/satori-go)

## 说明

本项目用于建立 Satori 服务端，通过规范化 API 使用 QQ 官方机器人。QQ 通信、鉴权、事件转换和媒体上传由 satori-go 负责；GlycCat 提供配置、日志、运行管理、消息缓存和已观察会话目录，不再内置旧版 botgo。

当前依赖固定为 satori-go `v1.3.2-0.20260924155532-f705ea27882c` 与 botgo-plus `v0.2.0`。botgo-plus 的旧 `v1.1.0` 属于另一条历史接口线，不能仅按版本号大小升级回去。构建不依赖旁边的源码目录或本机绝对路径 replace。

当前分支仍处于迁移修复阶段。下文的“实现”表示代码中存在对应链路，不代表真实 QQ 账号已获权限或已经完成生产联调。未完成事项见文末，不能把构建通过当成功能验收完成。

### 接口

- [x] [HTTP API](https://satori.chat/zh-CN/protocol/api.html)
- [x] [WebSocket 事件](https://satori.chat/zh-CN/protocol/events.html)
- [x] [WebHook 事件](https://satori.chat/zh-CN/protocol/events.html)

QQ 开放平台接入与 Satori 下游事件传输是两条链路：`account.websocket` / `account.webhook` 控制 QQ 接入，必须二选一；Satori 的 `/v1/events` 与反向 WebHook 面向机器人应用，不能将两边的 Token 和地址混用。

## 使用

### 构建与启动

需要 Go 1.25.4。先在项目根目录构建，首次运行显式初始化配置：

```powershell
go build -o GlycCat.exe .
.\GlycCat.exe -config config.yml -init
.\GlycCat.exe -config config.yml
```

Linux 和 macOS 使用对应的可执行文件，命令参数相同。普通启动只读取配置；缺少文件时会提示初始化，不会自动进入问答或改写文件。`-init` 不覆盖已有配置；迁移配置使用 `-update-config`，原文件会保留为同目录中的独立备份。

```powershell
.\GlycCat.exe -config config.yml -update-config
```

`-debug` 启用调试日志，`-faststart` 跳过启动环境提示。配置可以使用其他路径，但 `data/` 和 `log/` 仍相对于当前工作目录，请固定启动目录，避免误用另一套缓存。程序启动、配置或退出清理失败时返回非零退出码。

### 账号与监听

机器人需要 `app_id` 和 `app_secret`。旧 `bot_id`、`account.token` 仅为兼容配置保留，不再作为运行前提。Satori 的 `token` 是独立访问令牌；监听非本机地址时必须设置。

默认 Satori 地址为 `127.0.0.1:5140`，QQ WebHook 路径为 `/qqbot`。QQ 回调监听地址或端口为空/零时沿用 Satori 配置；完全相同的端点共享监听，重叠但不相同的地址会报错。单独回调入口与 Satori 入口可能均注册平台回调路径，公开部署应在反向代理明确限制可访问路径。

内建监听提供 HTTP。填写 443 不会自动启用 HTTPS，公网 HTTPS 应由反向代理终止；不要把全部 API 路由连同平台回调一起匿名公开。IPv6 地址只填主机部分，不在 host 中附加端口。

WebSocket 默认由 SDK 自动启动网关返回的完整分片集合。旧 `shards` 不再直接映射到单片配置；手动分片必须同时填写 `shard_id` 和 `shard_count`，编号从 0 开始且小于总数。`intents` 是 WebSocket 订阅，不会代替开放平台的 WebHook 事件配置。

完整字段和默认值见 [配置模板](config/template.yml)。加载时保留显式零值，未知字段或不合法配置直接报错，不静默删除字段。

### Satori 请求

默认部署路径为空，标准 API 位于 `/v1/`。普通动作使用 POST，按配置提供以下请求头：

```text
Authorization: Bearer <Satori 访问令牌>
Satori-Platform: qq
Satori-User-ID: <meta 或 READY 中返回的 user.id>
Content-Type: application/json
```

`/v1/meta` 用于获取登录资料和代理信息，不要求平台身份头，但仍遵守 Satori Token 校验。下游 WebHook 通过 `/v1/meta/webhook.create` 和 `/v1/meta/webhook.delete` 管理；旧 `/admin/*` 路径不作为兼容接口提供。

账号标识以 SDK 返回的 `user.id` 为准，`qq` 与 `qqguild` 通过 platform 区分。不要再将配置 BotID 或 AppID 当作 self_id，也不要把临时 `login.sn` 当成持久账号键。QQ 私聊频道使用 `private:<UserOpenID>`，群聊频道使用实际群 OpenID。

### 被动回复与引用

回复事件时将 `event.referrer` 原样传入 `message.create.referrer`；继续回复同一上下文时使用上一条发送结果返回的 referrer。SDK 已处理 `msg_id`、`event_id` 和调用内的序号推进，GlycCat 不建立另一套计数器。

```json
{
  "channel_id": "private:<UserOpenID>",
  "content": "收到消息",
  "referrer": {
    "msg_id": "<来源消息 ID>",
    "msg_seq": -1,
    "direct": true,
    "app_id": "<AppID>"
  }
}
```

示例仅说明结构，实际应透传完整 referrer。`<qq:passive id="..." seq="..."/>` 仍由 SDK 支持；不要写成 `<passive>`。展示引用使用 `<quote id="..."/>`，它不是被动回复来源。旧 `<quote><message id="..."/></quote>` 的应用兼容尚未恢复，调用方暂需改为标准 quote.id。

一条调用被拆分发送时，后段失败可能返回非 2xx 响应及已经成功的 `messages`。GlycCat 保留原状态和已发送结果；不要因整次调用报错便重发全部内容。缓存写入失败只记录错误，不会将已发送消息伪装成未发送。

### 消息缓存与迁移

新库位于 `data/db/messages-v2`，按 AppID、平台、self_id、会话和消息 ID 隔离。仅记录应用收到或 SDK 明确返回已发送的内容，不提供 QQ 平台的完整历史。关闭 `database.message_database.enable` 时，本地历史与已观察目录能力一并停用；频道平台的原生查询不受影响。

消息分页按时间和稳定 ID 排序，支持 before、after、around 及 asc/desc。`limit` 默认 50；配置 limit=0 表示不附加配置层上限，仍受每页最多 1000 条和 32 MiB 消息 JSON 的本地保护。before/after 不包含锚点，around 包含存在的锚点；游标仅能用于对应账号和会话。

已观察群组/会话目录不是平台全量列表，也不保证离线期间的状态实时。退出事件会标记会话非活动，私聊不会进入群组列表；旧消息导入不会把历史群组自动标记成仍在群内。

旧 `data/db/messages` 不会在启动时自动转换或删除。需要迁移时，先停机并备份旧库，确认其 AppID 与新版 self_id，再显式运行：

```powershell
.\GlycCat.exe -migrate-messages "D:\Backups\messages" -migration-target "data\db\messages-v2" -migration-app-id "<AppID>" -migration-self-id "<新版 self_id>"
```

迁移只读源库，写独立新库，已有记录不覆盖。只有旧类型明确为 private 的记录才增加私聊前缀；缺字段或解码失败会计入失败数量，不猜测账号。支持中断后重跑，返回统计与失败状态。未完成真实旧库验证前，应保留备份，不把该命令当作已经过生产验证的一键升级。

### 文件与媒体

普通媒体使用 SDK 的 `upload.create` 上传字节，或提供 HTTP/HTTPS、data URI 与本服务生成的 internal 资源。RPC 不允许直接读取服务端任意 `file://` 路径；客户端本地文件应先上传。

SDK 临时上传默认上限为 32 MiB，临时资源保留约 10 分钟，服务关闭时清理；这些是本地实现限制，不是 QQ 官方配额。跨账号 internal 资源会被拒绝，完整可读取资源链接应妥善保管。

旧文件服务已经退出运行链路，相关无调用源码已移除；`file_server` 字段只为加载旧配置保留。不会启动时清理旧 `data/files`，也不会把旧数据库路径公开挂载。旧内部链接不能直接当作新版临时资源使用，应由客户端重新上传原始文件；不承诺长期文件托管与旧 file_info 缓存复用。

图片、视频和音频辅助包已经增加大小限制、临时目录隔离及外部程序超时，但自动发送前的媒体预处理尚未接回，当前发送路径不会自动调用这些转码函数。请先提供平台可接受的编码，不要将“上传成功”理解为“已经自动转码”。

辅助函数中视频和音频转码需要 PATH 中的 ffmpeg，最长处理时间为 60 秒，并有输出上限；不忽略当前目录可执行文件的安全检查。内嵌 SILK 编码器的 macOS 与 Windows 文件目前只有 amd64 可用，arm64 不能假定能原生执行。Linux 编码器仍有系统动态库依赖。辅助函数的真实输出和各平台执行效果尚待验证，不据此扩大支持承诺。

### 实现

🟩 表示存在 SDK 原生转换/请求链路，🟨 表示 GlycCat 本机缓存，🟥 表示当前不支持。所有原生操作仍取决于平台权限及场景；缓存必须启用，且只覆盖实际观察到的记录。

<details>
<summary>消息元素</summary>

| 元素标签 | 功能 | QQ 普通子频道 | QQ 单聊/群聊 |
| --- | --- | :---: | :---: |
| 纯文本 | 文本消息 | 🟩 | 🟩 |
| `<at>`、`<sharp>` | 平台提及/文本转换 | 由 SDK 转换 | 平台显示效果需联调 |
| `<img>` | 图片 | 🟩 | 🟩 |
| `<audio>` | 语音 | 🟥 | 🟩 |
| `<video>` | 视频 | 🟥 | 🟩 |
| `<file>` | 文件 | 🟥 | 🟩 |
| `<quote id="..."/>` | 展示引用 | 🟩 | 由 SDK 按场景转换 |
| `<qq:passive>` | 被动回复扩展 | 🟩 | 🟩 |

其他平台扩展由所固定的 SDK 处理。频道私信的本地图片 multipart 目前返回 501，不能拿普通子频道上传路径代替；公网图片 URL 走 SDK 对应路径。

</details>

<details>
<summary>API 与能力来源</summary>

| API | QQ 普通子频道 | QQ 单聊/群聊 |
| --- | :---: | :---: |
| `message.create`、`message.delete` | 🟩 | 🟩 |
| `message.get`、`message.list` | 🟩 | 🟨 |
| `message.update` | 🟩 | 🟥 |
| `channel.get`、`channel.list` | 🟩 | 🟨 |
| `channel.create`、`channel.update`、`channel.delete` | 🟩 | 🟥 |
| `user.channel.create` | SDK 现有链路，私聊资源一致性待修 | 🟩 |
| `guild.get` | 🟩 | 🟩，实际群组信息接口 |
| `guild.list` | 🟩 | 🟨，仅已观察群组 |
| `guild.member.get`、`guild.member.list` | 🟩 | 🟩，群组场景 |
| `guild.member.kick`、`guild.member.mute` | 🟩 | 🟩，群组场景 |
| `guild.member.role.set`、`guild.member.role.unset` | 🟩 | 🟥 |
| `guild.role.list/create/update/delete` | 🟩 | 🟥 |
| `reaction.create/delete/list` | 🟩 | 🟥 |
| `login.get`、`upload.create` | 🟩 | 🟩 |

频道原生消息列表当前支持 before/after，around 返回 501。频道私信的消息读取和编辑另有限制。未列出的接口按 SDK 的实际 404/501/权限错误处理，不虚构成功结果。应用不会用陈旧缓存掩盖原生群资料或成员管理的权限错误。

</details>

<details>
<summary>事件与状态</summary>

QQ WebSocket 与 WebHook 均进入 SDK 事件转换，GlycCat 只通过一个事件转发点观察和缓存。群组、成员、消息、表态、登录和互动等事件由 SDK 按实际平台事件转换；未完整映射的原生信息通过 `_type` / `_data` 保留。

GlycCat 对 QQ 消息创建/删除、群组加入/更新/退出、好友加入/移除及已创建私聊更新本机记录。缓存完成一次处理后继续上报原事件，不另开消费者抢读 SDK 通道。账号和事件流序号由 SDK 处理，不把缓存数据库当成可靠事件队列。

QQ 回调成功应答不代表消息已持久化，也不保证下游业务成功。没有跨进程去重、崩溃恢复或自动补发；平台重复投递也不能理解为 SDK 承诺恰好一次交付。互动默认由 SDK 应答，应用不要重复 ACK。

</details>

## 开发与当前边界

日常可直接使用已有 Go 命令构建和静态检查：

```sh
go build ./...
go vet ./...
go mod verify
```

GlycCat 当前没有功能测试，运行 `go test ./...` 只提供包编译检查。SDK 自带测试与 GlycCat 的业务集成验证是不同范围。发行配置按 go.mod 选择工具链，固定 GoReleaser 版本，不在发布时整理依赖；CI 发布任务是否通过以实际运行结果为准。

以下事项仍未完成，不应作为已验收功能：

- 自动发送前的媒体预处理与旧嵌套引用兼容尚未接回；本轮只修复辅助函数，未运行真实转码器。
- `satori.webhook.timeout` 目前仍是保留字段，没有传入服务端。实际推送由 SDK 处理：未指定订阅超时时默认 300 秒，不能把字段中的 10 秒当作生效，也不能把 0 当作无限等待。
- 固定 SDK 的 WebHook 注册失败状态回滚、频道私聊创建返回的 ID/类型一致性，以及 SDK 自建 HTTP 监听的读头/空闲保护仍待独立修复；公网部署需在代理层提供相应限制。
- 新消息库、分页和迁移尚未经过新增功能测试；真实 QQ 权限、频控、媒体效果及跨平台运行也没有以构建结果替代验证。

保留 [LICENSE](LICENSE)。内嵌编解码程序沿用项目已有文件，其实际平台依赖、来源和发行条件需单独核验，不以主程序能够构建代替核验。
