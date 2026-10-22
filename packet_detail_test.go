package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"packet_cloud/biz/handler"
	packet "packet_cloud/biz/model/hertz/packet"
	"packet_cloud/internal/cryptoutil"
	"packet_cloud/internal/storage"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

const detailPacketJSON = `{
	"id":1,"region":"跨3A","name":"原始云包","channel":"12",
	"uploader":"tester","time":"2026-10-22",
	"user_packets":[
		{"id":9,"name":"first","content":"00 ff\n<script>&\"","size":7,"send_timing":"before"},
		{"name":"zero","content":"零 ID 内容","size":4,"send_timing":"during"},
		{"id":9,"name":"duplicate","content":"unchanged","size":9,"send_timing":"after"}
	]
}`

func newPacketDetailServer(t *testing.T, token string) (*server.Hertz, *storage.LocalFileSystem, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "packets.json")
	repo, err := storage.NewLocalFileSystem(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	var p packet.CloudPacket
	if err := json.Unmarshal([]byte(detailPacketJSON), &p); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePacket([]*packet.CloudPacket{&p}); err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.Use(handler.WithRepository(repo))
	// 无令牌时不安装鉴权中间件，验证详情处理器自身设置缓存头。
	if token != "" {
		h.Use(handler.AuthorizeWrites(token))
	}
	register(h)
	return h, repo, path
}

func assertPacketDetailResponse(t *testing.T, r *ut.ResponseRecorder, status int, wantJSON string) {
	t.Helper()
	if r.Code != status {
		t.Fatalf("status=%d, want=%d, body=%s", r.Code, status, r.Body)
	}
	if got := r.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control=%q, want no-store", got)
	}
	if got := r.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type=%q, want application/json", got)
	}
	var got, want any
	if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v: %s", err, r.Body)
	}
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%s\nwant=%s", r.Body, wantJSON)
	}
}

func TestPacketDetailPreservesFullPacketAndStorage(t *testing.T) {
	h, _, path := newPacketDetailServer(t, "")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"code":0,"msg":"获取云数据包成功","packet":` + detailPacketJSON + `}`
	for _, url := range []string{
		"/v1/packet/detail/1",
		"/v1/packet/detail/1?id=1",
		"/v1/packet/detail/1?id=1&id=01",
		"/v1/packet/detail/01?id=%2B1",
		"/v1/packet/detail/+1?id=01",
	} {
		t.Run(url, func(t *testing.T) {
			r := ut.PerformRequest(h.Engine, "GET", url, nil)
			assertPacketDetailResponse(t, r, 200, want)
		})
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("detail reads changed persisted data")
	}
}

func TestPacketDetailReadIgnoresUploadToken(t *testing.T) {
	h, _, _ := newPacketDetailServer(t, "test-upload-secret")
	for _, authorization := range []string{"", "Bearer wrong", "Bearer test-upload-secret"} {
		t.Run(authorization, func(t *testing.T) {
			r := ut.PerformRequest(h.Engine, "GET", "/v1/packet/detail/1", nil,
				ut.Header{Key: "Authorization", Value: authorization})
			assertPacketDetailResponse(t, r, 200,
				`{"code":0,"msg":"获取云数据包成功","packet":`+detailPacketJSON+`}`)
		})
	}
}

func TestPacketDetailIDErrorsMatchLegacyGet(t *testing.T) {
	h, _, _ := newPacketDetailServer(t, "")
	for _, tc := range []struct {
		suffix string
		status int
		msg    string
	}{
		{"0", 400, "id 必须为有效的正整数"},
		{"-1", 400, "id 必须为有效的正整数"},
		{"nope", 400, "id 必须为有效的正整数"},
		{"1.0", 400, "id 必须为有效的正整数"},
		{"2147483648", 400, "id 必须为有效的正整数"},
		{"999999999999999999999999", 400, "id 必须为有效的正整数"},
		{"1?id=2", 400, "查询参数 id 必须与路径 id 一致"},
		{"1?id=1&id=2", 400, "查询参数 id 必须与路径 id 一致"},
		{"1?id=2&id=1", 400, "查询参数 id 必须与路径 id 一致"},
		{"1?id=", 400, "查询参数 id 必须与路径 id 一致"},
		{"1?id=1&id=", 400, "查询参数 id 必须与路径 id 一致"},
		{"1?id=nope", 400, "查询参数 id 必须与路径 id 一致"},
		{"1?id=0", 400, "查询参数 id 必须与路径 id 一致"},
		{"1?id=-1", 400, "查询参数 id 必须与路径 id 一致"},
		{"1?id=2147483648", 400, "查询参数 id 必须与路径 id 一致"},
		{"999", 404, "数据包不存在"},
		{"2147483647?id=2147483647", 404, "数据包不存在"},
	} {
		t.Run(tc.suffix, func(t *testing.T) {
			want, err := json.Marshal(map[string]any{"code": tc.status, "msg": tc.msg})
			if err != nil {
				t.Fatal(err)
			}
			r := ut.PerformRequest(h.Engine, "GET", "/v1/packet/detail/"+tc.suffix, nil)
			assertPacketDetailResponse(t, r, tc.status, string(want))
			legacy := ut.PerformRequest(h.Engine, "GET", "/v1/packet/get/"+tc.suffix, nil)
			if legacy.Code != r.Code || !bytes.Equal(legacy.Body.Bytes(), r.Body.Bytes()) {
				t.Errorf("legacy response differs: status=%d body=%s", legacy.Code, legacy.Body)
			}
		})
	}
}

func TestPacketDetailEmptyChildren(t *testing.T) {
	for _, tc := range []struct {
		name     string
		children []*packet.UserPacket
	}{
		{"nil", nil},
		{"empty", []*packet.UserPacket{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo, _ := newPacketDetailServer(t, "")
			p := &packet.CloudPacket{
				Id: 2147483647, Name: "empty", Region: "region", Channel: "1",
				Uploader: "tester", Time: "2026", UserPackets: tc.children,
			}
			if err := repo.SavePacket([]*packet.CloudPacket{p}); err != nil {
				t.Fatal(err)
			}
			r := ut.PerformRequest(h.Engine, "GET", "/v1/packet/detail/2147483647", nil)
			assertPacketDetailResponse(t, r, 200,
				`{"code":0,"msg":"获取云数据包成功","packet":{"id":2147483647,"name":"empty","region":"region","channel":"1","uploader":"tester","time":"2026"}}`)
		})
	}
}

func TestPacketDetailStorageFailuresAreSanitized(t *testing.T) {
	for _, corruption := range []string{"[null]", "{invalid JSON"} {
		t.Run(corruption, func(t *testing.T) {
			h, _, path := newPacketDetailServer(t, "")
			if err := os.WriteFile(path, []byte(corruption), 0600); err != nil {
				t.Fatal(err)
			}
			r := ut.PerformRequest(h.Engine, "GET", "/v1/packet/detail/1", nil)
			assertPacketDetailResponse(t, r, 500,
				`{"code":500,"msg":"存储操作失败，请稍后重试"}`)
			if strings.Contains(r.Body.String(), path) || strings.Contains(r.Body.String(), corruption) {
				t.Fatal("public error exposed storage details")
			}
		})
	}
	t.Run("path-bearing backend error", func(t *testing.T) {
		h, _, path := newPacketDetailServer(t, "")
		if err := os.Rename(path, path+".original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		r := ut.PerformRequest(h.Engine, "GET", "/v1/packet/detail/1", nil)
		assertPacketDetailResponse(t, r, 500,
			`{"code":500,"msg":"存储操作失败，请稍后重试"}`)
		if strings.Contains(r.Body.String(), path) {
			t.Fatal("public error exposed storage path")
		}
	})
}

func TestPacketDetailDoesNotReplaceLegacyEncryption(t *testing.T) {
	h, repo, _ := newPacketDetailServer(t, "test-upload-secret")
	r := ut.PerformRequest(h.Engine, "GET", "/v1/packet/get/1", nil)
	var response packet.GetPacketByIDResp
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if r.Code != 200 || response.UserPackets == "" || strings.Contains(r.Body.String(), `"packet":`) {
		t.Fatalf("legacy get is not encrypted: status=%d body=%s", r.Code, r.Body)
	}
	plain, err := cryptoutil.AESCBCDecrypt([]byte(response.UserPackets))
	if err != nil {
		t.Fatal(err)
	}
	p, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var gotJSON, wantJSON any
	if err := json.Unmarshal([]byte(plain), &gotJSON); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("legacy content changed: %s", plain)
	}
}
