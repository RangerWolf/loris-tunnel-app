package devtunnel

import (
	"fmt"
	"strconv"
	"strings"
)

// NormalizeExpiration accepts an empty value, hours, or days.
// Empty means the default of 30 days. The CLI allows 1 hour through 30 days.
func NormalizeExpiration(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "30d", nil
	}
	if len(value) < 2 {
		return "", expirationRangeError()
	}
	unit := value[len(value)-1]
	raw := value[:len(value)-1]
	for _, r := range raw {
		if r < '0' || r > '9' {
			return "", expirationRangeError()
		}
	}
	number, err := strconv.Atoi(raw)
	if err != nil || number < 1 {
		return "", expirationRangeError()
	}
	switch unit {
	case 'h':
		if number > 30*24 {
			return "", expirationRangeError()
		}
	case 'd':
		if number > 30 {
			return "", expirationRangeError()
		}
	default:
		return "", expirationRangeError()
	}
	return strconv.Itoa(number) + string(unit), nil
}

func expirationRangeError() error {
	return fmt.Errorf("dev tunnel expiration must be 1-720 hours or 1-30 days")
}
