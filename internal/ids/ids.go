package ids

import (
	"crypto/rand"
	"encoding/base64"
)

func New(prefix string) string {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	s := base64.RawURLEncoding.EncodeToString(b[:])
	if prefix == "" {
		return s
	}
	return prefix + "_" + s
}
