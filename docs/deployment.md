# 部署、备份与数据库迁移

[返回 README](../README.md)

## 构建与启动

```bash
make build
PACKET_CONFIG=/absolute/path/config.json ./output/packet-cloud
```

`output/packet-cloud` 已包含管理页和图标，单独分发二进制即可提供页面。JSON 配置和数据库需另外准备；Go 构建产物适用于构建时的操作系统和 CPU。配置文件和数据相对路径取决于启动工作目录，部署建议使用绝对数据路径。

默认监听 `127.0.0.1:8080`，可通过 `PACKET_HOST_PORTS` 修改。后台删除无需令牌，上传令牌不保护删除接口。`/healthz` 仅用于进程存活检查。

服务启动时打开一个 Repository，退出时关闭连接池。首次启动失败会明确退出，不会静默切换存储。配置变更需重启，前台进程用 Ctrl-C 停止。

## 本地文件

数据为云包 JSON 数组。默认 `./packets`，首次缺失时为空集合，首次写入创建文件；父目录须存在。仓库样例位于 `examples/packets.json`，真实运行数据不要提交。

一个文件只允许一个进程/实例写入。文件写入通过锁、临时文件 sync、原子 rename 完成；目录 sync 为尽力执行，不保证所有硬件故障下的持久性。正在运行时不要手动修改数据文件。损坏数据会报错，写操作不会以空集合覆盖它。

备份应在停写后复制文件。存储包保留即时 `Backup` 方法生成同目录时间戳副本，但没有自动定时备份。恢复前停止服务，用有效数组替换文件，再重启。多实例运行使用 MySQL。

## MySQL 新安装

先创建数据库并配置主库 DSN。启动账户须有表结构初始化权限，新表由 GORM 初始化。`db/schema.sql` 固定创建并使用 `packet_cloud`；使用自定义库名时可由服务初始化表结构，或先调整脚本中的库名。`ReadDSN` 与缓存配置仅作兼容，当前读取主库保证写后可见。

```bash
STORAGE_MEDIA=mysql \
MYSQL_DSN='user:password@tcp(127.0.0.1:3306)/packet_cloud?charset=utf8mb4&parseTime=true&loc=Local' \
./output/packet-cloud
```

## 从旧结构升级

旧父包 ID 没有 AUTO_INCREMENT，旧子包以客户端 ID 为全局主键。新版会在启动前检测并拒绝旧结构，须明确执行迁移。

1. 用数据库工具完成备份，停止所有旧实例和写入方。
2. 在维护窗口选定目标数据库，执行迁移 002。需 DDL 和存储过程权限；MySQL DDL 隐式提交。
3. 确认父包 ID 可自动分配、子包主键为 `(cloud_packet_id, ordinal)`，核对行数和内容。
4. 启动新版，验证详情与上传/删除，再恢复写入。

```bash
mysql -h HOST -u USER -p packet_cloud < db/migrations/002_storage_identity.sql
```

迁移支持重复执行。旧数据库未保存数组位置，只能按子包 ID 还原历史顺序；新数据保留实际数组顺序。已经丢失的历史子包无法通过迁移恢复。`001_init.sql` 是历史结构，不用于新版初始化。

回滚前同样停写、备份，执行 `002_storage_identity.down.sql`。只有子包 ID 全局唯一且数组顺序可由 ID 还原时脚本才允许回滚，否则明确拒绝，应保留新版或恢复升级前备份。不要通过去重或修改客户端 ID 强行回滚。

测试环境命令见[开发文档](development.md)。测试只允许显式 `TEST_MYSQL_DSN` 的 `_test` 数据库，不使用应用 DSN。
