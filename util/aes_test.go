package util

import (
    "testing"
)

func TestAESRoundTrip(t *testing.T) {
    src := []byte("hello")
    enc, err := AESCBCEncrypt(src)
    if err != nil {
        t.Fatalf("enc: %v", err)
    }
    dec, err := AESCBCDecrypt([]byte(enc))
    if err != nil {
        t.Fatalf("dec: %v", err)
    }
    if dec != string(src) {
        t.Fatalf("mismatch: %s", dec)
    }
}