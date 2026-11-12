package application

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func previewSignature(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}
