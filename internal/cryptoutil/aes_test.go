package cryptoutil

import (
	"encoding/base64"
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

func TestAESRejectsMalformedCiphertext(t *testing.T) {
	for _, n := range []int{0, 1, 15, 16, 17, 31, 33} {
		t.Run(string(rune('A'+n)), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("decrypt panicked for length %d: %v", n, r)
				}
			}()
			input := base64.StdEncoding.EncodeToString(make([]byte, n))
			if _, err := AESCBCDecrypt([]byte(input)); err == nil {
				t.Errorf("accepted malformed ciphertext length %d", n)
			}
		})
	}
	if _, err := AESCBCDecrypt([]byte("%%%")); err == nil {
		t.Fatal("accepted invalid base64")
	}
}

func TestAESRejectsInvalidPadding(t *testing.T) {
	encrypted, err := AESCBCEncrypt([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	// CBC: altering the last IV byte changes the last plaintext padding byte.
	raw[15] ^= 11
	if _, err := AESCBCDecrypt([]byte(base64.StdEncoding.EncodeToString(raw))); err == nil {
		t.Fatal("accepted zero padding")
	}
}
