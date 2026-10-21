package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	packet "packet_cloud/biz/model/hertz/packet"
	"packet_cloud/internal/cryptoutil"
	"packet_cloud/internal/storage"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

const validUpload = `{"cloud_packet":{"name":"测试","region":"跨1","channel":"1","uploader":"tester","time":"2026-10-21","user_packets":[{"id":0,"content":"original"}]}}`

func testServer(t *testing.T, token string) (*server.Hertz, storage.Repository) {
	t.Helper()
	repo, err := storage.NewLocalFileSystem(filepath.Join(t.TempDir(), "packets"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	h := server.New()
	h.Use(WithRepository(repo), AuthorizeWrites(token))
	h.POST("/v1/packet/upload", UploadPacket)
	h.POST("/v1/packet/mupload", MUploadAllChannelsPacket)
	h.GET("/v1/packet/list", ListPacket)
	h.GET("/v1/packet/get/:id", GetPacketByID)
	h.DELETE("/v1/packet/delete", DeletePacket)
	return h, repo
}

func request(h *server.Hertz, method, path, body, token string) *ut.ResponseRecorder {
	return ut.PerformRequest(h.Engine, method, path,
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "Authorization", Value: token})
}

func TestUploadListAndPathIDPreserveContent(t *testing.T) {
	h, _ := testServer(t, "")
	if r := request(h, "POST", "/v1/packet/upload", validUpload, ""); r.Code != 200 {
		t.Fatalf("upload: %d %s", r.Code, r.Body)
	}
	if r := request(h, "GET", "/v1/packet/list", "", ""); r.Code != 200 || strings.Contains(r.Body.String(), "original") {
		t.Fatalf("list: %d %s", r.Code, r.Body)
	}
	r := request(h, "GET", "/v1/packet/get/1", "", "")
	var response packet.GetPacketByIDResp
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if r.Code != 200 || response.UserPackets == "" {
		t.Fatalf("get path ID: %d %s", r.Code, r.Body)
	}
	plain, err := cryptoutil.AESCBCDecrypt([]byte(response.UserPackets))
	if err != nil || !strings.Contains(plain, "original") {
		t.Fatalf("content lost after list: %s %v", plain, err)
	}
	for _, path := range []string{"/v1/packet/get/0", "/v1/packet/get/nope", "/v1/packet/get/2147483648", "/v1/packet/get/1?id=2", "/v1/packet/get/1?id=1&id=2"} {
		if r := request(h, "GET", path, "", ""); r.Code != 400 {
			t.Errorf("%s: %d", path, r.Code)
		}
	}
	if r := request(h, "GET", "/v1/packet/get/999", "", ""); r.Code != 404 {
		t.Errorf("missing: %d", r.Code)
	}
}

func TestRejectedMutationsDoNotChangeStorage(t *testing.T) {
	h, repo := testServer(t, "test-secret")
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"POST", "/v1/packet/upload", validUpload, "", 401},
		{"POST", "/v1/packet/upload", validUpload, "Bearer wrong", 401},
		{"POST", "/v1/packet/mupload", `{}`, "Bearer test-secret", 400},
		{"POST", "/v1/packet/mupload", `{"mcloud_packet":null}`, "Bearer test-secret", 400},
		{"POST", "/v1/packet/mupload", `{"mcloud_packet":{"channel":[]}}`, "Bearer test-secret", 400},
		{"POST", "/v1/packet/upload", strings.Replace(validUpload, `{"id":0,"content":"original"}`, "null", 1), "Bearer test-secret", 400},
		{"POST", "/v1/packet/upload", strings.Replace(validUpload, "测试", strings.Repeat("长", 65), 1), "Bearer test-secret", 400},
		{"DELETE", "/v1/packet/delete", `{"from":0,"to":1}`, "", 400},
		{"DELETE", "/v1/packet/delete", `{"from":2,"to":1}`, "", 400},
	} {
		r := request(h, tc.method, tc.path, tc.body, tc.token)
		if r.Code != tc.status {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, r.Code, r.Body)
		}
		var response struct {
			Code int
			Msg  string
		}
		if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil || response.Code == 0 || response.Msg == "" {
			t.Errorf("unusable error response: %s", r.Body)
		}
	}
	items, err := repo.List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("rejected mutations changed storage: %v %v", items, err)
	}
	if r := request(h, "POST", "/v1/packet/upload", validUpload, "Bearer test-secret"); r.Code != 200 {
		t.Fatalf("authorized upload: %d %s", r.Code, r.Body)
	}
	if r := request(h, "GET", "/v1/packet/list", "", ""); r.Code != 200 {
		t.Fatalf("public read: %d", r.Code)
	}
	if r := request(h, "DELETE", "/v1/packet/delete", `{"from":1,"to":1}`, ""); r.Code != 200 {
		t.Fatalf("delete: %d", r.Code)
	}
	items, err = repo.List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("delete not applied: %v %v", items, err)
	}
}

func TestPersistedCorruptionIsServerFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packets")
	repo, err := storage.NewLocalFileSystem(path)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	h := server.New()
	h.Use(WithRepository(repo))
	h.GET("/v1/packet/list", ListPacket)
	if err := os.WriteFile(path, []byte("[null]"), 0600); err != nil {
		t.Fatal(err)
	}
	r := request(h, "GET", "/v1/packet/list", "", "")
	if r.Code != 500 {
		t.Fatalf("corrupt storage status=%d body=%s", r.Code, r.Body)
	}
	if strings.Contains(r.Body.String(), path) {
		t.Fatal("public error exposed storage path")
	}
}
