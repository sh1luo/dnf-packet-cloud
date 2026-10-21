.PHONY: run debug build test race vet check mysql-up mysql-down integration

# Existing generator entry points; generated protobuf/routes are not hand-edited.
init_api:
	hz new -I idl -idl idl/packet/packet.proto -module packet_cloud

update_api:
	hz update -I idl -idl idl/packet/packet.proto

update_api2:
	hertztool -stack=true update -I=idl -idl=idl/packet/packet.proto

run:
	go run .

# Isolated local debug data; never writes the tracked packets fixture.
debug:
	mkdir -p .runtime
	STORAGE_MEDIA=lfs PACKETS_FILE_PATH=.runtime/packets PACKET_HOST_PORTS=127.0.0.1:8080 go run .

build:
	mkdir -p output
	go build -trimpath -o output/packet-cloud .

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

check: vet race build

mysql-up:
	docker compose up -d --wait mysql

mysql-down:
	docker compose down

# This target only targets the dedicated disposable database below.
integration:
	TEST_MYSQL_DSN='packet_test:packet_test@tcp(127.0.0.1:33067)/packet_cloud_test?parseTime=true&charset=utf8mb4' go test -count=1 -v ./service/readwriter
