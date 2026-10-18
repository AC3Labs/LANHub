package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// Encrypter reads and writes values in Laravel's Crypt format, so the Go
// hub can share encrypted columns (agent tokens, 2FA secrets, settings)
// with the PHP hub using the same APP_KEY:
//
//	base64( json{ "iv": b64, "value": b64(AES-256-CBC ciphertext), "mac": hex(HMAC-SHA256(key, iv_b64 + value_b64)), "tag": "" } )
//
// Values are encrypted without PHP serialization, matching Eloquent's
// "encrypted" cast (encryptString).
type Encrypter struct {
	key []byte
}

type laravelPayload struct {
	IV    string `json:"iv"`
	Value string `json:"value"`
	MAC   string `json:"mac"`
	Tag   string `json:"tag"`
}

var errDecrypt = errors.New("the value could not be decrypted")

// NewEncrypter takes APP_KEY as written in Laravel's .env ("base64:...").
func NewEncrypter(appKey string) (*Encrypter, error) {
	raw := strings.TrimSpace(appKey)
	if !strings.HasPrefix(raw, "base64:") {
		return nil, errors.New("APP_KEY must be a base64: key")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(raw, "base64:"))
	if err != nil || len(key) != 32 {
		return nil, errors.New("APP_KEY must decode to 32 bytes (AES-256-CBC)")
	}
	return &Encrypter{key: key}, nil
}

func (e *Encrypter) mac(ivB64, valueB64 string) string {
	m := hmac.New(sha256.New, e.key)
	m.Write([]byte(ivB64 + valueB64))
	return hex.EncodeToString(m.Sum(nil))
}

func (e *Encrypter) DecryptString(payload string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", errDecrypt
	}
	var p laravelPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.IV == "" || p.Value == "" {
		return "", errDecrypt
	}
	if !hmac.Equal([]byte(e.mac(p.IV, p.Value)), []byte(p.MAC)) {
		return "", errors.New("the MAC is invalid")
	}
	iv, err := base64.StdEncoding.DecodeString(p.IV)
	if err != nil || len(iv) != aes.BlockSize {
		return "", errDecrypt
	}
	ct, err := base64.StdEncoding.DecodeString(p.Value)
	if err != nil || len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return "", errDecrypt
	}
	block, _ := aes.NewCipher(e.key)
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)
	pad := int(plain[len(plain)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(plain) {
		return "", errDecrypt
	}
	for _, b := range plain[len(plain)-pad:] {
		if int(b) != pad {
			return "", errDecrypt
		}
	}
	return string(plain[:len(plain)-pad]), nil
}

func (e *Encrypter) EncryptString(value string) (string, error) {
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	pad := aes.BlockSize - len(value)%aes.BlockSize
	plain := append([]byte(value), []byte(strings.Repeat(string(rune(pad)), pad))...)
	block, _ := aes.NewCipher(e.key)
	ct := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, plain)

	ivB64 := base64.StdEncoding.EncodeToString(iv)
	valueB64 := base64.StdEncoding.EncodeToString(ct)
	// Laravel json_encode()s with escaped slashes; PHP's decrypt doesn't
	// care either way, but keep the same shape.
	out, err := json.Marshal(laravelPayload{IV: ivB64, Value: valueB64, MAC: e.mac(ivB64, valueB64), Tag: ""})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(out), nil
}
