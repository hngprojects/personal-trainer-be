package auth

import (
    "crypto/rand"
    "math/big"
)

const passwordChars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%"

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
