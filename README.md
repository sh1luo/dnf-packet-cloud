# 云包在线管理工具（Packet Cloud）

基于 Go / Hertz 的数据包上传、分发与管理服务，支持本地 JSON 文件和 MySQL。

## 能力与边界

- 单包上传、按固定区域映射批量上传频道、摘要列表及加密详情。
- 管理页支持搜索、区域筛选、分页、多选、全选和删除所选记录。
- 可选上传令牌；**读取和后台删除无需令牌**，删除保留二次确认与范围校验。
- 保留旧客户端 AES-CBC 协议；本地文件仅支持单进程写入，多实例使用 MySQL。
- 页面和图标编译进二进制，无需 Node.js 或单独分发 HTML。

## 快速开始

macOS / Linux 需要 Go 1.24 或更新版本及 `make`。在仓库根目录运行：

```bash
make debug
# 管理页：http://127.0.0.1:8080/v1/packet/edit
# 存活探针：http://127.0.0.1:8080/healthz
```

默认调试数据为 `.runtime/packets`，目标不存在时使用空集合；按 Ctrl-C 退出。上传一条示例：

```bash
curl -X POST http://127.0.0.1:8080/v1/packet/upload \
  -H 'Content-Type: application/json' \
  --data-binary @examples/upload.json
```

启用了 `PACKET_WRITE_TOKEN` 时，上传还需 `Authorization: Bearer <token>`。

其他常用入口：

```bash
make              # 显示帮助
make demo         # 仅在目标数据文件不存在时从 examples/packets.json 初始化
make check        # 格式检查、vet、race 测试、构建
make build        # 生成 output/packet-cloud
```

`make run` 使用常规配置，默认数据路径仍为 `./packets`；该文件不随仓库分发且被 Git 忽略，新克隆默认没有数据。二进制已嵌入页面，但配置和数据的相对路径仍相对于**进程工作目录**。

## 管理页面

打开 `/v1/packet/edit`，可按 ID、名称、上传者或频道搜索，结合区域筛选及分页查看记录。

- **多选与全选**：使用每行左侧复选框选择记录，表头复选框全选或取消当前页；「全选筛选结果」包含其他页的匹配记录。搜索、区域筛选与翻页保留选择，操作栏会提示未显示的已选数量；「取消选择」清空全部选择。
- **删除所选**：点击列表上方红色「删除所选」按钮。勾选一条即可单条删除，勾选多条即可批量删除，无需逐行操作或填写 ID 范围。手机横向滚动时选择列保持可见。
- **删除确认与失败处理**：弹窗展示已选数量及完整 ID 清单；取消不会发送请求。仅删除勾选的 ID，服务端确认成功后才移除对应记录。若中途失败，显示已完成与剩余数量，保留失败及未处理记录的选择，重试仅处理剩余 ID。删除无需令牌。

页面为当前加载时的快照，点击「刷新列表」获取最新记录。修改 `web/` 内嵌资源后需重新构建并重启服务。

## 仓库结构

```text
.
|-- main.go / router.go / router_gen.go  # Hertz 启动、自定义路由、生成路由入口
|-- biz/
|   |-- handler/                        # HTTP 逻辑，含自定义 online_edit.go
|   |-- model/                          # 生成的 protobuf 模型
|   `-- router/                         # 生成路由及中间件入口
|-- idl/                                # 接口定义与生成依赖
|-- internal/
|   |-- config/                         # 配置加载与校验
|   |-- storage/                        # Repository、LFS、MySQL 与测试
|   |   `-- testdata/legacy_packets.json # 旧存储测试数据
|   `-- cryptoutil/                     # 旧客户端 CBC 协议
|-- web/
|   |-- assets.go                       # go:embed 资源入口
|   |-- templates/packet/online_edit.html
|   `-- static/favicon.ico
|-- config/config.json                  # 运行时默认配置
|-- db/                                 # 当前 schema 与版本化迁移
|-- examples/                           # packets.json 数据集、upload.json 请求
|-- scripts/                            # 本地启动与格式检查
|-- docs/                               # 专题文档、images/legacy-admin.png 历史截图
|-- .github/workflows/go.yml             # CI
|-- docker-compose.yml                  # 本地 MySQL 测试环境
|-- Makefile                            # 开发、校验、构建与生成命令
|-- go.mod / go.sum                      # Go 依赖
|-- .hz / .hertztool                     # 生成器元数据
`-- README.md / CONTRIBUTING.md          # 项目入口与贡献约定
```

运行生成的 `.runtime/`、`output/` 和根目录 `packets` 不属于源码。`examples/packets.json` 保存原根目录示例，历史截图仅作归档，不代表当前界面。

## 文档导航

- [架构与代码边界](docs/architecture.md)
- [本地开发、测试与代码生成](docs/development.md)
- [配置项与环境变量](docs/configuration.md)
- [HTTP API 与兼容协议](docs/api.md)
- [部署、备份与数据库迁移](docs/deployment.md)
- [贡献约定](CONTRIBUTING.md)
