.DEFAULT_GOAL := help
.PHONY: help run debug demo build fmt fmt-check test race vet check mysql-up mysql-down integration generate init_api update_api update_api2

help:
	@printf '%s\n' \
	  'Packet Cloud 开发命令' \
	  '  make debug       空数据本地调试（默认 127.0.0.1:8080）' \
	  '  make demo        使用示例数据调试，不覆盖已有数据' \
	  '  make run         使用常规配置启动' \
	  '  make build       构建 output/packet-cloud（内嵌页面）' \
	  '  make fmt         格式化手写 Go 代码' \
	  '  make check       格式、vet、race、构建' \
	  '  make mysql-up    启动本地 MySQL 测试环境' \
	  '  make integration 运行独立 MySQL 集成测试' \
	  '  make mysql-down  停止测试环境' \
	  '  make generate    从 IDL 更新生成代码（需要 hz v0.8.1）'

# Existing generator entry points; generated protobuf/routes are not hand-edited.
init_api:
	hz new -I idl -idl idl/packet/packet.proto -module packet_cloud

update_api:
	hz update -I idl -idl idl/packet/packet.proto

update_api2:
	hertztool -stack=true update -I=idl -idl=idl/packet/packet.proto

generate: update_api

run:
	go run .

# Local runtime data is separate from examples/.
debug:
	sh scripts/run-local.sh debug

demo:
	sh scripts/run-local.sh demo

build:
	mkdir -p output
	go build -trimpath -o output/packet-cloud .

fmt:
	sh scripts/check-format.sh fix

fmt-check:
	sh scripts/check-format.sh check

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

check: fmt-check vet race build

mysql-up:
	docker compose up -d --wait mysql

mysql-down:
	docker compose down

# This target only targets the dedicated disposable database below.
integration:
	TEST_MYSQL_DSN='packet_test:packet_test@tcp(127.0.0.1:33067)/packet_cloud_test?parseTime=true&charset=utf8mb4' go test -count=1 -v ./internal/storage
