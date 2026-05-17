package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

type config struct {
	ServerAddr         string
	LogLevel           string
	DatabaseURL        string
	RedisURL           string
	JWTSecret          string
	Installed          bool
	ReadHeaderTimeout  time.Duration
	WWTIOTProviderCode string
	WWTIOTProviderName string
	WWTIOTAPIURL       string
	WWTIOTUserID       string
	WWTIOTUserKey      string
	WWTIOTTimeout      time.Duration
}

func loadConfig(envFiles ...string) (config, error) {
	if err := loadEnvFiles(envFiles...); err != nil {
		return config{}, err
	}

	cfg := config{
		ServerAddr:         envDefault("SERVER_ADDR", ":8080"),
		LogLevel:           envDefault("LOG_LEVEL", "info"),
		DatabaseURL:        strings.TrimSpace(os.Getenv("DATABASE_URL")),
		RedisURL:           strings.TrimSpace(os.Getenv("REDIS_URL")),
		JWTSecret:          strings.TrimSpace(os.Getenv("JWT_SECRET")),
		Installed:          envBool("DEVICE_PLATFORM_INSTALLED", false),
		ReadHeaderTimeout:  envDuration("READ_HEADER_TIMEOUT", 5*time.Second),
		WWTIOTProviderCode: envDefault("WWTIOT_PROVIDER_CODE", "wwtiot"),
		WWTIOTProviderName: envDefault("WWTIOT_PROVIDER_NAME", "WWTIOT"),
		WWTIOTAPIURL:       envDefault("WWTIOT_API_URL", "http://gps.wwtiot.com/api/"),
		WWTIOTUserID:       strings.TrimSpace(os.Getenv("WWTIOT_USER_ID")),
		WWTIOTUserKey:      strings.TrimSpace(os.Getenv("WWTIOT_USER_KEY")),
		WWTIOTTimeout:      envDuration("WWTIOT_TIMEOUT", 10*time.Second),
	}
	if cfg.isInstalled() {
		if cfg.DatabaseURL == "" {
			return config{}, fmt.Errorf("DATABASE_URL must not be empty after installation")
		}
		if cfg.RedisURL == "" {
			return config{}, fmt.Errorf("REDIS_URL must not be empty after installation")
		}
		if len(cfg.JWTSecret) < minJWTSecretLength {
			return config{}, fmt.Errorf("JWT_SECRET must be at least %d characters after installation", minJWTSecretLength)
		}
	}
	return cfg, nil
}

func (cfg config) isInstalled() bool {
	return cfg.Installed || installLockExists()
}

func loadEnvFiles(paths ...string) error {
	for _, path := range paths {
		if err := loadEnvFile(path); err != nil {
			return err
		}
	}
	return nil
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open env file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "export ") && strings.TrimSpace(strings.TrimPrefix(line, "export ")) == "" {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		value = strings.TrimSpace(stripInlineEnvComment(value))
		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read env file %s: %w", path, err)
	}
	return nil
}

func envDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func stripInlineEnvComment(value string) string {
	inSingle := false
	inDouble := false
	for i, r := range value {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
				return value[:i]
			}
		}
	}
	return value
}
