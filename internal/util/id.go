package util

import (
	"crypto/rand"
	"encoding/hex"
)

// Возвращает 128-битный случайный hex ID.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// очень маловероятно; возвращаем нули
	}
	return hex.EncodeToString(b[:])
}
