# 贡献约定

从 [README](README.md) 了解运行方式与目录，再阅读[架构说明](docs/architecture.md)。

- 保留现有 API 路径与客户端加密协议；不兼容变更须明确说明。
- 请求处理在 `biz/handler/`，存储与原子写入在 `internal/storage/`，配置默认值集中在 `internal/config/`。
- 自定义路由使用 `router.go`，不要手改生成的模型和路由。生成流程见[开发文档](docs/development.md)。
- 新代码按职责放入现有目录，不为单个功能创建无用途的层或公共包。
- 测试与对应代码放在同包，文件读写使用临时目录。数据库测试只能使用显式 `_test` 数据库。
- 页面在 `web/templates/`、静态文件在 `web/static/`，修改后重新构建；无需前端依赖安装。
- 更新功能时同步维护 API、配置或部署文档。示例放在 `examples/`，真实数据、私有配置、构建产物和日志不提交。

提交前运行：

```bash
make fmt
make check
git diff --check
```

存储变更另外运行 `make mysql-up && make integration`，完成后 `make mysql-down`。涉及 SQL 迁移时在独立数据库验证升级、重复执行及回滚拒绝条件。提交描述写清问题、行为变化和实际验证结果。
