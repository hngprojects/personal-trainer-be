package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// ErrPasswordTooLong is returned when the input would be silently truncated by
// bcrypt. bcrypt only hashes the first 72 bytes; longer inputs would otherwise
// quietly authenticate any password that shares the same first 72 bytes.
var ErrPasswordTooLong = errors.New("password exceeds 72-byte bcrypt limit")

const maxPasswordBytes = 72

func HashPassword(plain string) (string, error) {
	if len([]byte(plain)) > maxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(h), err
}

func CheckPassword(hash, plain string) error {
	if len([]byte(plain)) > maxPasswordBytes {
		return ErrPasswordTooLong
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}
