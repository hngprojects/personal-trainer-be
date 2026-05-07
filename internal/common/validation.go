package common

import "regexp"

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

func IsValidEmail(email string) bool {
	return emailRegex.MatchString(email)
}

func IsNotEmpty(value string) bool {
	return value != ""
}

func MinLength(value string, min int) bool {
	return len(value) >= min
}

func MaxLength(value string, max int) bool {
	return len(value) <= max
}
