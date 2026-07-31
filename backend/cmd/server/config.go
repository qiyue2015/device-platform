package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/webhookworker"
)

type config struct {
	ServerAddr                 string
	LogLevel                   string
	DatabaseURL                string
	RedisURL                   string
	JWTSecret                  string
	WebhookSecretEncryptionKey []byte
	WebhookWorkerInterval      time.Duration
	WebhookRequestTimeout      time.Duration
	WebhookLeaseDuration       time.Duration
	WebhookMaxAttempts         int
	WebhookRetrySchedule       []time.Duration
	WebhookEgressAllowlist     string
	Installed                  bool
	ReadHeaderTimeout          time.Duration
	WWTIOTAPIURL               string
	WWTIOTUserID               string
	WWTIOTUserKey              string
}

func loadConfig(envFiles ...string) (config, error) {
	if err := loadEnvFiles(envFiles...); err != nil {
		return config{}, err
	}

	webhookWorkerInterval, err := strictPositiveDuration("WEBHOOK_WORKER_INTERVAL", 2*time.Second)
	if err != nil {
		return config{}, err
	}
	webhookRequestTimeout, err := strictPositiveDuration("WEBHOOK_REQUEST_TIMEOUT", 10*time.Second)
	if err != nil {
		return config{}, err
	}
	webhookLeaseDuration, err := strictPositiveDuration("WEBHOOK_LEASE_DURATION", 15*time.Second)
	if err != nil {
		return config{}, err
	}
	if webhookLeaseDuration <= webhookRequestTimeout {
		return config{}, fmt.Errorf("WEBHOOK_LEASE_DURATION must be greater than WEBHOOK_REQUEST_TIMEOUT")
	}
	webhookMaxAttempts, err := strictIntRange("WEBHOOK_MAX_ATTEMPTS", 5, 1, 5)
	if err != nil {
		return config{}, err
	}
	webhookRetrySchedule, err := strictWebhookRetrySchedule(webhookMaxAttempts)
	if err != nil {
		return config{}, err
	}
	webhookEgressAllowlist := strings.TrimSpace(os.Getenv("WEBHOOK_EGRESS_ALLOWLIST"))
	if _, err := webhookworker.ParseEgressAllowlist(webhookEgressAllowlist); err != nil {
		return config{}, fmt.Errorf("WEBHOOK_EGRESS_ALLOWLIST is invalid: %w", err)
	}

	cfg := config{
		ServerAddr:             envDefault("SERVER_ADDR", ":8080"),
		LogLevel:               envDefault("LOG_LEVEL", "info"),
		DatabaseURL:            strings.TrimSpace(os.Getenv("DATABASE_URL")),
		RedisURL:               strings.TrimSpace(os.Getenv("REDIS_URL")),
		JWTSecret:              strings.TrimSpace(os.Getenv("JWT_SECRET")),
		Installed:              installLockExists(),
		ReadHeaderTimeout:      envDuration("READ_HEADER_TIMEOUT", 5*time.Second),
		WebhookWorkerInterval:  webhookWorkerInterval,
		WebhookRequestTimeout:  webhookRequestTimeout,
		WebhookLeaseDuration:   webhookLeaseDuration,
		WebhookMaxAttempts:     webhookMaxAttempts,
		WebhookRetrySchedule:   webhookRetrySchedule,
		WebhookEgressAllowlist: webhookEgressAllowlist,
		WWTIOTAPIURL:           envDefault("WWTIOT_API_URL", "http://gps.wwtiot.com/api/"),
		WWTIOTUserID:           strings.TrimSpace(os.Getenv("WWTIOT_USER_ID")),
		WWTIOTUserKey:          os.Getenv("WWTIOT_USER_KEY"),
	}
	webhookEncryptionKey, err := decodeWebhookEncryptionKey(os.Getenv("WEBHOOK_SECRET_ENCRYPTION_KEY"))
	if err != nil {
		return config{}, err
	}
	cfg.WebhookSecretEncryptionKey = webhookEncryptionKey
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
		if len(cfg.WebhookSecretEncryptionKey) != 32 {
			return config{}, fmt.Errorf("WEBHOOK_SECRET_ENCRYPTION_KEY must decode to exactly 32 bytes after installation")
		}
	}
	return cfg, nil
}

func strictPositiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return value, nil
}

func strictIntRange(key string, fallback, minimum, maximum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", key, minimum, maximum)
	}
	return value, nil
}

func strictWebhookRetrySchedule(maxAttempts int) ([]time.Duration, error) {
	defaults := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute}
	raw := strings.TrimSpace(os.Getenv("WEBHOOK_RETRY_SCHEDULE"))
	if raw == "" {
		return append([]time.Duration(nil), defaults[:maxAttempts-1]...), nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) != maxAttempts-1 {
		return nil, fmt.Errorf("WEBHOOK_RETRY_SCHEDULE must contain WEBHOOK_MAX_ATTEMPTS minus one durations")
	}
	schedule := make([]time.Duration, len(parts))
	for index, part := range parts {
		value, err := time.ParseDuration(strings.TrimSpace(part))
		if err != nil || value < defaults[index] {
			return nil, fmt.Errorf("WEBHOOK_RETRY_SCHEDULE duration %d must be at least %s", index+1, defaults[index])
		}
		schedule[index] = value
	}
	return schedule, nil
}

func decodeWebhookEncryptionKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	key, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("WEBHOOK_SECRET_ENCRYPTION_KEY must be unpadded base64url encoding of exactly 32 bytes")
	}
	return key, nil
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
	values, err := readEnvValues(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}
	return nil
}

func readEnvValues(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open env file %s: %w", path, err)
	}
	defer file.Close()
	values := make(map[string]string)
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
		value = strings.TrimSpace(stripInlineEnvComment(value))
		if _, exists := values[key]; !exists {
			values[key] = strings.Trim(value, `"'`)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read env file %s: %w", path, err)
	}
	return values, nil
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
