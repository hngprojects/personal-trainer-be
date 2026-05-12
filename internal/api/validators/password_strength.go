package validators

import "unicode"

func ValidatePasswordStrength(pw string) (ok bool, msg string) {
	if len(pw) < 8 {
		return false, "password must be at least 8 characters"
	}
	var hasUpper, hasDigit, hasSpecial bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}
	if !hasUpper {
		return false, "password must contain at least one uppercase letter"
	}
	if !hasDigit {
		return false, "password must contain at least one number"
	}
	if !hasSpecial {
		return false, "password must contain at least one special character"
	}
	return true, ""
}
