package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// newUUID генерирует UUID версии 4.
func NewUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("не удалось сгенерировать UUID: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// base62Chars – алфавит для генерации коротких идентификаторов.
var base62Chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateID создаёт криптографически случайный идентификатор заданной длины.
func GenerateID(length int) (string, error) {
	id := make([]byte, length)
	for i := range id {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62Chars))))
		if err != nil {
			return "", fmt.Errorf("ошибка генерации случайного числа: %w", err)
		}
		id[i] = base62Chars[idx.Int64()]
	}
	return string(id), nil
}
