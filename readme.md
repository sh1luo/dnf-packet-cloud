# 云包在线管理工具

基于 Go / Hertz 的数据包上传、分发与管理服务，支持本地 JSON 文件和 MySQL。管理页包含搜索、区域筛选、分页、单条删除及 ID 范围删除；客户端详情保留原有 AES-CBC 数据格式。

## 本地开发（macOS / Linux）

需要 Go 1.24 或更新版本。运行命令时工作目录须为项目根目录，以加载 HTML 模板和配置。

```bash
make debug
# 管理页：http://127.0.0.1:8080/v1/packet/edit
# 存活探针：http://127.0.0.1:8080/healthz
```

`make debug` 使用 `.runtime/packets` 独立数据文件，首次运行自动创建空集合，不修改仓库中的 `packets` 示例。按 Ctrl-C 退出。`make run` 使用常规配置；`make build` 生成 `output/packet-cloud`，运行二进制时也需保留项目的 `html` 目录。

```bash
make test          # 不配置测试 DSN 时跳过 MySQL 集成测试
make check         # go vet、race 测试、构建
make mysql-up      # Docker 启动独立 MySQL 8.4.7，127.0.0.1:33067
make integration   # 仅连接 packet_cloud_test 测试库，清理本轮创建的数据
make mysql-down    # 停止测试容器，保留 Docker volume
```

CI 在 macOS、Linux 上检查格式、race、vet 与构建，并用独立 MySQL 服务运行集成测试。生成器命令 `init_api`、`update_api`、`update_api2` 保留；不要手改生成的 protobuf 或路由。

## 配置

默认加载 `config/config.json`，缺失时使用默认值；JSON 格式、未知字段或参数不合法会使启动失败。通过 `PACKET_CONFIG` 显式指定的配置文件必须存在。未填写的字段合并默认值，然后由环境变量覆盖。配置仅启动时加载，不支持热更新。

| 字段 | 环境变量 | 默认值 / 用途 |
| --- | --- | --- |
| `StorageMedia` | `STORAGE_MEDIA` | `lfs`，可选 `mysql` |
| `PacketsFilePath` | `PACKETS_FILE_PATH` | `./packets` |
| `HostPorts` | `PACKET_HOST_PORTS` | `127.0.0.1:8080` |
| `WriteToken` | `PACKET_WRITE_TOKEN` | 空，设置后上传接口要求 Bearer token；后台删除不需要令牌 |
| `MySQL.DSN` | `MYSQL_DSN` | MySQL 主库 DSN，mysql 模式必填 |
| `MySQL.ReadDSN` | `MYSQL_READ_DSN` | 兼容旧配置；当前全部读取主库保证写后可见 |

连接池、连接生命周期及超时参数见 `config/config.json`。`QueryCacheTTLms` 保留兼容但不启用 MySQL 快照缓存。私有配置可放 `config/dev.local.json`（已忽略）。

```bash
# 示例：令牌通过环境注入，实际部署请使用自己的值。
PACKET_WRITE_TOKEN=your-token make debug

# 数据库模式（须先创建数据库，并授予初始化/迁移表结构的权限）
STORAGE_MEDIA=mysql \
MYSQL_DSN='user:password@tcp(127.0.0.1:3306)/packet_cloud?charset=utf8mb4&parseTime=true&loc=Local' \
go run .
```

默认只监听本机。需要远程访问时显式设置 `PACKET_HOST_PORTS=0.0.0.0:8080`。启用上传令牌后，旧上传客户端需补充 `Authorization: Bearer <token>`。读取和后台删除不要求令牌；管理页直接确认删除，保留范围校验与失败提示。

## HTTP 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/v1/packet/list` | 摘要，不包含数据包内容 |
| GET | `/v1/packet/get/:id` | 完整数据包 JSON 经原有 CBC 格式加密，放入 `user_packets` |
| POST | `/v1/packet/upload` | `{"cloud_packet":{...}}`，由服务端生成云包 ID |
| POST | `/v1/packet/mupload` | `{"mcloud_packet":{...,"channel":[...]}}`，一次事务上传 1～9 个频道 |
| DELETE | `/v1/packet/delete` | `{"from":1,"to":10}`，包含两端的正整数范围，无需令牌 |
| GET | `/v1/packet/edit` | 管理页 |
| GET | `/healthz` | 进程存活探针，不代表数据库实时可用性 |

上传示例：

```bash
curl -X POST http://127.0.0.1:8080/v1/packet/upload \
  -H 'Content-Type: application/json' \
  -d '{"cloud_packet":{"region":"跨1","name":"示例","channel":"1","uploader":"dev","time":"2026-10-21","user_packets":[{"id":0,"name":"示例项","content":"data","size":4,"send_timing":""}]}}'
```

请求体上限 4 MiB。region/channel/time 最长 32 字符，name/uploader 最长 64 字符；不能为空白。每个云包最多 10000 个子包，内容合计最多 2 MiB。子包不得为 null，ID/size 非负，name 最长 64 字符，send_timing 最长 32 字符。允许空子包数组、零 ID 和重复子包 ID；子包顺序会保留。

批量频道仍按原位置映射为：跨1、跨2、跨3A、跨3B、跨4、跨5、跨6、跨7、跨8。提供部分数组表示该映射的前 N 项。

兼容性变更：详情 ID 以路径为准，同时提供 query `id` 时必须与路径一致。非法参数返回 HTTP 400，未找到返回 404，启用鉴权的上传接口令牌缺失/错误返回 401，存储失败返回 500，错误体统一为 `{"code":状态码,"msg":"说明"}`。批量上传成功从 `null` 改为 `{"code":0,"msg":"批量上传成功"}`，删除成功显式包含 `code:0`。客户端应同时检查 HTTP 状态与业务码。

## 数据与运维

本地文件存储在一个进程内共享实例，写入由锁串行化，通过同目录临时文件、sync、rename 原子替换。读取返回独立对象，不会因列表或管理页操作而改写包内容。文件元数据变化时重新加载；正在运行时不要手动改文件。**同一个文件只允许一个服务进程写入**，多实例部署使用 MySQL。损坏文件会明确报错，不会用空集合覆盖。

MySQL 在启动时建立连接池并初始化新表，上传和删除按目标记录事务执行，不再整库重写。已有旧表结构会阻止启动并提示迁移，不会静默修改旧主键。先备份数据库并停止所有旧版本写入，再执行 `db/migrations/002_storage_identity.sql`，最后启动新版。迁移需要 DDL/存储过程权限；MySQL DDL 会隐式提交，迁移脚本支持重入。`db/schema.sql` 提供新建表结构；`001_init.sql` 保留为历史版本。

```bash
mysql -h HOST -u USER -p packet_cloud < db/migrations/002_storage_identity.sql
```

新的子包主键为 `(cloud_packet_id, ordinal)`，客户端子包 ID 独立保存。旧数据没有保存数组位置，迁移只能按子包 ID 确定历史顺序；新写入保留原数组顺序。历史上已丢失的子包无法通过迁移恢复。MySQL 的旧 `SavePacket` 全量替换入口已停用，业务写入使用 `Append` / `DeleteRange`。

回滚脚本 `db/migrations/002_storage_identity.down.sql` 只适用于子包 ID 全局唯一且顺序可由 ID 还原的数据库；不满足时会明确拒绝。此时应保留新版结构或恢复升级前备份，不能通过去重强行回滚。执行前同样须停写和备份。

MySQL 测试仅使用显式 `TEST_MYSQL_DSN`，数据库名必须以 `_test` 结尾；绝不使用应用配置 DSN 作为测试回退。

详情保留 `base64(随机 IV || AES-CBC(PKCS#7(JSON)))` 与旧内置密钥，以兼容已有客户端。该格式不提供消息认证，内置密钥也不应当作访问控制；如需升级加密，应引入客户端可协商的新协议版本。
