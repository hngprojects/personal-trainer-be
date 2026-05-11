package auth

import (
	"crypto/rand"
	"errors"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

// ErrPasswordTooLong is returned when the input would be silently truncated by
// bcrypt. bcrypt only hashes the first 72 bytes; longer inputs would otherwise
// quietly authenticate any password that shares the same first 72 bytes.
var ErrPasswordTooLong = errors.New("password exceeds 72-byte bcrypt limit")

const (
	maxPasswordBytes = 72
	passwordChars    = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%"
)

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

func GenerateRandomPassword(length int) (string, error) {
	if length < 12 {
		length = 12
	}
	out := make([]byte, length)
	max := big.NewInt(int64(len(passwordChars)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = passwordChars[n.Int64()]
	}
	return string(out), nil
}
