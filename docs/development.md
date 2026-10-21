# 本地开发与测试

[返回 README](../README.md)

需要 Go 1.24+ 和 make；MySQL 实库测试另外需要 Docker Compose。前端使用原生 HTML/CSS/JavaScript，无 Node.js 构建步骤。

```bash
make                     # 命令帮助
make debug               # 独立 .runtime/packets，默认空集合
make demo                # 从 examples/packets.json 初始化不存在的数据文件
PACKET_HOST_PORTS=127.0.0.1:18080 make debug
PACKETS_FILE_PATH=.runtime/custom.json make demo
```

`scripts/run-local.sh` 自动进入仓库根目录，强制选择文件存储，保留自定义路径和端口。`demo` 不覆盖已有数据。前台 Ctrl-C 停止；`make run` 则使用常规配置，不强制调试模式。

```bash
make fmt                 # 格式化手写 Go 文件
make fmt-check
make test
make race
make vet
make build               # output/packet-cloud，页面和图标已嵌入
make check               # 格式、vet、race、构建
go test ./internal/storage -run '^$' -bench BenchmarkLFS -benchmem
```

测试与代码放在同一包。文件存储测试使用临时目录，`internal/storage/testdata/legacy_packets.json` 为历史样例；运行数据不提交 Git。Go 包移动后可用 `go list ./...` 核对导入关系。

## MySQL 实库

```bash
make mysql-up
make integration
make mysql-down
```

Compose 固定 MySQL 8.4.7，监听 `127.0.0.1:33067`，使用 `packet_cloud_test`。停止容器保留 volume。集成测试只接受显式 `TEST_MYSQL_DSN`，库名必须以 `_test` 结尾，不读取应用 DSN；只清理本轮测试标签的数据。

```bash
TEST_MYSQL_DSN='packet_test:packet_test@tcp(127.0.0.1:33067)/packet_cloud_test?parseTime=true&charset=utf8mb4' \
  go test -race -count=1 -v ./internal/storage
```

CI 在 macOS/Linux 执行 `make check`，另用 MySQL 服务验证存储。迁移脚本需要按[部署说明](deployment.md)单独验证，普通集成测试不自动演练旧库升级。

## 代码生成

`.hz` 记录 hz v0.8.1，`.hertztool` 记录历史 hertztool v3.3.1。生成器并非运行服务的必需依赖。

```bash
go install github.com/cloudwego/hertz/cmd/hz@v0.8.1
make generate            # 等价 make update_api
```

接口定义在 `idl/packet/packet.proto`。保留 `init_api` 和 `update_api2` 历史入口；已有仓库更新优先使用 `generate`，不要用初始化命令覆盖已有手写逻辑。生成后检查 diff、运行 `make check`，尤其核对 IDL、模型、路由及处理器是否一致。

不手改生成的 protobuf、`biz/router/packet/packet.go`、`router_gen.go` 或生成器元数据。自定义页面路由在 `router.go`，处理器在 `biz/handler/online_edit.go`。页面资源变更后需重启 `go run` 或重新构建，当前不提供模板热更新。
