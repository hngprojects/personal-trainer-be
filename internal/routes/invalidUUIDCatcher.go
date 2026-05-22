package routes

import "strings"

func extractUUIDParamName(err error) string {
	name := err.Error()
	errorPrefix := "Invalid format for parameter "
	if !strings.HasPrefix(name, errorPrefix) {
		return "parameter is unknown"
	}
	name = strings.TrimPrefix(name, errorPrefix)
	if idx := strings.Index(name, ":"); idx != -1 {
		name = name[:idx]
	}
	return strings.TrimSpace(name)
}
