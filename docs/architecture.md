# 架构与代码边界

[返回 README](../README.md)

服务沿用 Hertz 和现有 protobuf 接口。手写基础模块归入 `internal/`，生成模型与路由保持 Hertz 原有目录。

```text
main.go
  ├─ internal/config：默认配置 → 文件 → 环境覆盖 → 校验 → 配置副本
  ├─ internal/storage.Open：启动时打开一个 Repository
  ├─ handler.WithRepository / AuthorizeWrites：注入存储、可选上传鉴权
  ├─ web.Register：嵌入模板与图标
  └─ register：生成 API 路由 + 自定义管理页、存活探针

HTTP → biz/handler → Repository → 本地文件 / MySQL
详情 → internal/cryptoutil → 兼容客户端的加密响应
```

| 目录 / 文件 | 职责 |
| --- | --- |
| `biz/handler/` | 绑定请求、参数校验、响应与管理页渲染 |
| `internal/storage/` | 云包 ID 分配、原子追加/删除、持久化及资源生命周期 |
| `internal/config/` | 集中处理默认值、文件和环境变量，成功后发布配置快照 |
| `internal/cryptoutil/` | 旧客户端 CBC 格式，不承载访问控制 |
| `web/` | `go:embed` 模板和静态资源；更新页面后重新构建 |
| `biz/model/`、`biz/router/`、`router_gen.go` | 生成代码，不手工修改 |
| `config/`、`db/`、`examples/` | 运行配置、数据库结构、示例数据 |

Repository 提供 `List`、`Get`、`Append`、`DeleteRange`、`Close`。处理器只使用这些操作，不做读取后整份覆盖。返回对象属于调用方，列表仅包含摘要，页面不会改写原始子包内容。

本地文件以实例锁串行处理读改写，临时文件写入并 sync 后 rename。文件元数据变化会触发重新读取；一个文件只允许一个进程和实例写入。ID 高水位在实例内保留，重启按现存最大 ID 恢复。

MySQL 使用一个主库连接池，上传/删除事务只操作目标数据，详情在 repeatable-read 事务中读取父子包。云包 ID 使用 AUTO_INCREMENT，子包主键为 `(cloud_packet_id, ordinal)`，客户端 ID 单独保存。旧全量 `SavePacket` MySQL 入口停用，保留的兼容包装不用于 HTTP 请求。

根目录入口保留是为了兼容现有生成器。此次目录整理不更换框架、不修改 API 路径、不新增无用途的分层。
