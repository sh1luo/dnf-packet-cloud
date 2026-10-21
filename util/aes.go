package util

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var key = []byte{84, 72, 73, 82, 73, 83, 77, 90, 83, 69, 67, 82, 69, 84, 75, 69}

func AESCBCEncrypt(src []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	blockSize := block.BlockSize()
	plaintext := Padding(src, blockSize)
	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext[aes.BlockSize:], plaintext)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// AESCBCDecrypt preserves the legacy wire format. CBC does not authenticate
// ciphertext; transport security and write authorization are separate concerns.
func AESCBCDecrypt(src []byte) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(string(src))
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < 2*aes.BlockSize || len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("invalid ciphertext length")
	}
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)
	ciphertext, err = unpad(ciphertext, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(ciphertext), nil
}

func Padding(plainText []byte, blockSize int) []byte {
	if blockSize < 1 || blockSize > 255 {
		return nil
	}
	padding := blockSize - len(plainText)%blockSize
	padded := make([]byte, len(plainText)+padding)
	copy(padded, plainText)
	copy(padded[len(plainText):], bytes.Repeat([]byte{byte(padding)}, padding))
	return padded
}

// UnPadding returns nil for malformed PKCS#7 data.
func UnPadding(plainText []byte) []byte {
	result, _ := unpad(plainText, aes.BlockSize)
	return result
}

func unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid padding")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, errors.New("invalid padding")
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, errors.New("invalid padding")
		}
	}
	return data[:len(data)-padding], nil
}
