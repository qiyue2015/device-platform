package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

type config struct {
	ServerAddr        string
	LogLevel          string
	AdminEmail        string
	AdminPassword     string
	AdminAccessToken  string
	OpenAPIKeys       map[string]string
	ReadHeaderTimeout time.Duration
}

func loadConfig(envFiles ...string) (config, error) {
	if err := loadEnvFiles(envFiles...); err != nil {
		return config{}, err
	}

	cfg := config{
		ServerAddr:        envDefault("SERVER_ADDR", ":8080"),
		LogLevel:          envDefault("LOG_LEVEL", "info"),
		AdminEmail:        envDefault("ADMIN_EMAIL", "admin@example.com"),
		AdminPassword:     envDefault("ADMIN_PASSWORD", "local-admin-password"),
		AdminAccessToken:  envDefault("ADMIN_ACCESS_TOKEN", "dev-admin-token"),
		OpenAPIKeys:       parseOpenAPIKeys(envDefault("OPEN_API_KEYS", "local-project:local-open-api-key")),
		ReadHeaderTimeout: envDuration("READ_HEADER_TIMEOUT", 5*time.Second),
	}
	if cfg.AdminEmail == "" {
		return config{}, fmt.Errorf("ADMIN_EMAIL must not be empty")
	}
	if cfg.AdminPassword == "" {
		return config{}, fmt.Errorf("ADMIN_PASSWORD must not be empty")
	}
	if cfg.AdminAccessToken == "" {
		return config{}, fmt.Errorf("ADMIN_ACCESS_TOKEN must not be empty")
	}
	return cfg, nil
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

func parseOpenAPIKeys(raw string) map[string]string {
	keys := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		projectID, apiKey, ok := strings.Cut(part, ":")
		if !ok {
			projectID = "default"
			apiKey = part
		}
		projectID = strings.TrimSpace(projectID)
		apiKey = strings.TrimSpace(apiKey)
		if projectID == "" || apiKey == "" {
			continue
		}
		keys[apiKey] = projectID
	}
	return keys
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
