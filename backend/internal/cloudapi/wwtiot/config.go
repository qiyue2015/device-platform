package wwtiot

import (
	"os"
	"strings"
)

func ConfigFromEnv() Config {
	return Config{
		APIURL:  envDefault("WWTIOT_API_URL", "http://gps.wwtiot.com/api"),
		UserID:  os.Getenv("WWTIOT_USER_ID"),
		UserKey: os.Getenv("WWTIOT_USER_KEY"),
		DryRun:  envBool("WWTIOT_DRY_RUN", true),
	}
}

func envDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
