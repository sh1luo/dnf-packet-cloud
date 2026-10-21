# HTTP API 与客户端协议

[返回 README](../README.md)

默认地址 `http://127.0.0.1:8080`。读取及后台删除无需令牌；配置 `WriteToken` 后，两种上传接口要求 `Authorization: Bearer <token>`。

| 方法 | 路径 | 请求与响应 |
| --- | --- | --- |
| GET | `/v1/packet/list` | `cloud_packets` 摘要，无子包内容 |
| GET | `/v1/packet/get/:id` | `user_packets` 为完整云包 JSON 的加密字符串 |
| POST | `/v1/packet/upload` | `{"cloud_packet":{...}}`，服务端分配云包 ID |
| POST | `/v1/packet/mupload` | `{"mcloud_packet":{...,"channel":[...]}}`，事务批量上传 |
| DELETE | `/v1/packet/delete` | `{"from":1,"to":10}`，正整数范围，含两端 |
| GET | `/v1/packet/edit` | 后台管理页 |
| GET | `/healthz` | `{"status":"ok"}`，进程存活，不代表数据库实时可用 |

## 示例

```bash
curl -X POST http://127.0.0.1:8080/v1/packet/upload \
  -H 'Content-Type: application/json' \
  --data-binary @examples/upload.json

curl http://127.0.0.1:8080/v1/packet/list
curl http://127.0.0.1:8080/v1/packet/get/1

curl -X DELETE http://127.0.0.1:8080/v1/packet/delete \
  -H 'Content-Type: application/json' \
  -d '{"from":1,"to":1}'
```

上传请求体见 [examples/upload.json](../examples/upload.json)。批量上传将同样的元数据放入 `mcloud_packet`，把 `channel` 改为字符串数组，区域依次映射为跨1、跨2、跨3A、跨3B、跨4、跨5、跨6、跨7、跨8；部分数组表示前 N 个区域，数组长度必须为 1～9。

## 参数约束

HTTP 请求体上限 4 MiB。云包 region/channel/time 必须为非空白且不超过 32 字符，name/uploader 不超过 64 字符。`user_packets` 必须为数组，可为空，最多 10000 项，内容合计不超过 2 MiB。

子包不能为 null；ID/size 非负，name 最长 64 字符，send_timing 最长 32 字符。零 ID 和重复子包 ID 均保留，存储使用独立 ordinal 维持数组顺序。客户端传入的云包 ID 不用于分配。

详情路径 ID 必须为正整数 int32；如同时提供 query `id`，每个值均须与路径一致。删除 `from`、`to` 同样为正整数 int32，且 `from <= to`。

## 状态与响应

错误统一为 `{"code":HTTP状态码,"msg":"说明"}`：400 参数错误、401 上传令牌缺失/错误、404 未找到、409 ID 耗尽、500 存储或内部错误。后台删除不要求令牌，页面仅在 HTTP 成功及 `code:0` 时更新列表。

上传、列表、详情保留原生成模型的成功字段；其中 `code:0` 可能因 `omitempty` 被省略。批量上传和删除成功显式返回 `{"code":0,"msg":"..."}`。无数据时 `cloud_packets` 也可能省略，客户端按空数组处理；数据内容仅通过详情获取。

旧版本行为变化：详情路径现在正确绑定，未找到返回 404；批量成功从 `null` 改为业务响应；错误不再序列化为 `{}`。客户端需同时检查 HTTP 状态与对应响应字段。

## 加密格式

详情响应保留 `base64(随机 16 字节 IV || AES-CBC(PKCS#7(JSON)))` 及旧内置密钥，解密后为完整 `CloudPacket` JSON。工具实现拒绝错误 base64、截断块和非法 padding。

CBC 不提供消息认证，内置密钥仅用于客户端兼容。新格式应通过客户端可协商的协议版本引入，不能直接替换现有响应。
