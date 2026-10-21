# 配置项与环境变量

[返回 README](../README.md)

配置加载顺序：默认值 → JSON 文件 → 环境变量 → 校验 → 发布只读副本。配置只在启动时加载，变更后重启。

默认文件为 `config/config.json`，缺失时使用默认值；`PACKET_CONFIG` 显式指定的文件必须存在。未知字段、显式 null、无效参数会使启动失败。私有文件可放 `config/dev.local.json`，已被 Git 忽略。

| JSON 字段 | 环境变量 | 默认值 / 行为 |
| --- | --- | --- |
| `StorageMedia` | `STORAGE_MEDIA` | `lfs`，或 `mysql` |
| `PacketsFilePath` | `PACKETS_FILE_PATH` | `./packets` |
| `HostPorts` | `PACKET_HOST_PORTS` | `127.0.0.1:8080` |
| `WriteToken` | `PACKET_WRITE_TOKEN` | 空；设置后仅上传要求 Bearer，后台删除无需令牌 |
| `MySQL.DSN` | `MYSQL_DSN` | mysql 模式必填，连接主库 |
| `MySQL.ReadDSN` | `MYSQL_READ_DSN` | 兼容旧配置，当前读取始终走主库 |
| `MySQL.MaxOpen` | — | 20，必须大于 0 |
| `MySQL.MaxIdle` | — | 10，范围 0～MaxOpen |
| `MySQL.ConnMaxLifetimeMin` | — | 30，连接生命周期，分钟 |
| `MySQL.QueryTimeoutMs` | — | 3000，操作与连接超时上界，毫秒 |
| `MySQL.QueryCacheTTLms` | — | 500，兼容字段，当前未启用 MySQL 缓存 |
| `MySQL.SlowQueryMs` | — | 200，兼容字段，当前不用于慢查询阈值日志 |

非空上传令牌须为可见 ASCII，不能包含空白或控制字符。监听端口须在 1～65535 范围。

```bash
PACKET_CONFIG=config/dev.local.json go run .

STORAGE_MEDIA=mysql \
MYSQL_DSN='user:password@tcp(127.0.0.1:3306)/packet_cloud?charset=utf8mb4&parseTime=true&loc=Local' \
./output/packet-cloud
```

**路径规则**：相对配置路径和数据路径均相对于进程工作目录，数据路径不相对于配置文件所在目录。页面和图标已嵌入二进制，不受工作目录影响。部署时可显式使用绝对路径。

根目录 `packets` 现在是被忽略的运行数据；原始样例保存在 `examples/packets.json`。已有部署的文件路径和数据继续可用，新克隆使用空集合；`make demo` 可将样例初始化到独立调试文件。
