//go:build integration

package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const processServerHelperEnv = "DEVICE_PLATFORM_PROCESS_SERVER_HELPER"

type processServer struct {
	command  *exec.Cmd
	baseURL  string
	done     chan struct{}
	stderr   *bytes.Buffer
	killOnce sync.Once
}

type processEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Meta    struct {
		IdempotentReplay bool `json:"idempotent_replay"`
	} `json:"meta"`
}

type processCommandResponse struct {
	ID string `json:"id"`
}

func TestMultiProcessServerHelper(t *testing.T) {
	if os.Getenv(processServerHelperEnv) != "1" {
		return
	}
	cfg := config{
		ServerAddr:                 "127.0.0.1:0",
		DatabaseURL:                os.Getenv("DEVICE_PLATFORM_PROCESS_DATABASE_URL"),
		RedisURL:                   os.Getenv("DEVICE_PLATFORM_PROCESS_REDIS_URL"),
		JWTSecret:                  testJWTSecret,
		WebhookSecretEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		WebhookEgressAllowlist:     "127.0.0.0/8",
		Installed:                  true,
		ReadHeaderTimeout:          5 * time.Second,
	}
	application, err := newApp(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.close()
	listener, err := net.Listen("tcp", cfg.ServerAddr)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: application.routes(), ReadHeaderTimeout: cfg.ReadHeaderTimeout}
	fmt.Printf("READY %s\n", listener.Addr().String())
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}

func TestInstalledAppUsesOnlyPersistentRuntimeServices(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		cfg := config{
			DatabaseURL:                processTestDatabaseURL(t, db),
			RedisURL:                   requireProcessRedisTestURL(t),
			JWTSecret:                  testJWTSecret,
			WebhookSecretEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
			Installed:                  true,
			ReadHeaderTimeout:          5 * time.Second,
		}
		application, err := newApp(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		defer application.close()

		if application.deviceService != nil || application.commandRouter != nil || application.gateway != nil || application.webhooks != nil {
			t.Fatal("installed app created parallel in-memory runtime services")
		}
		if application.projects == nil || application.devices == nil || application.commands == nil || application.simulator == nil || application.webhookAudit == nil {
			t.Fatal("installed app did not initialize all persistent runtime services")
		}
	})
}

func TestMultipleServerProcessesShareIdempotencyAndWorkerOwnership(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		seedAuthTestAdmin(t, db)
		databaseURL := processTestDatabaseURL(t, db)
		redisURL := requireProcessRedisTestURL(t)
		var webhookCalls atomic.Int64
		webhookEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			webhookCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer webhookEndpoint.Close()
		first := startProcessServer(t, databaseURL, redisURL)
		defer first.kill(t)
		second := startProcessServer(t, databaseURL, redisURL)
		defer second.kill(t)

		token := processLogin(t, first.baseURL)
		projectID := processCreateProject(t, first.baseURL, token, webhookEndpoint.URL)
		deviceID := processCreateSimulatorDevice(t, first.baseURL, token, projectID)
		waitForProcessState(t, 5*time.Second, func() (bool, error) {
			var deliveryStatus string
			var attemptCount int
			err := db.QueryRow(`
				SELECT d.status, count(a.id)
				FROM webhook_deliveries d
				JOIN device_events e ON e.id = d.event_id
				LEFT JOIN webhook_delivery_attempts a ON a.delivery_id = d.id
				WHERE e.device_id = $1 AND e.event_type = 'device.created'
				GROUP BY d.id
			`, deviceID).Scan(&deliveryStatus, &attemptCount)
			return deliveryStatus == "delivered" && attemptCount == 1 && webhookCalls.Load() == 1, err
		})
		processPatchSimulator(t, first.baseURL, token, "provider_accepted", 250)

		body := commandCreateBody(projectID, deviceID, "query_status", "multi-process-idempotency", `{}`)
		start := make(chan struct{})
		type result struct {
			status   int
			envelope processEnvelope
			err      error
		}
		results := make(chan result, 2)
		for _, baseURL := range []string{first.baseURL, second.baseURL} {
			go func(baseURL string) {
				<-start
				status, envelope, err := processJSONRequest(baseURL, token, http.MethodPost, "/v1/device-commands", body)
				results <- result{status: status, envelope: envelope, err: err}
			}(baseURL)
		}
		close(start)

		statuses := map[int]int{}
		commandIDs := map[string]int{}
		replays := 0
		for range 2 {
			item := <-results
			if item.err != nil {
				t.Fatal(item.err)
			}
			statuses[item.status]++
			if item.envelope.Meta.IdempotentReplay {
				replays++
			}
			var command processCommandResponse
			if err := json.Unmarshal(item.envelope.Data, &command); err != nil || command.ID == "" {
				t.Fatalf("decode Command response: command=%+v err=%v", command, err)
			}
			commandIDs[command.ID]++
		}
		if statuses[http.StatusCreated] != 1 || statuses[http.StatusOK] != 1 || replays != 1 || len(commandIDs) != 1 {
			t.Fatalf("cross-process idempotency statuses=%v replays=%d command_ids=%v", statuses, replays, commandIDs)
		}
		var commandID string
		for commandID = range commandIDs {
		}
		waitForProcessState(t, 5*time.Second, func() (bool, error) {
			var commands, attempts, completed int
			err := db.QueryRow(`
				SELECT
					(SELECT count(*) FROM device_commands WHERE idempotency_key = 'multi-process-idempotency'),
					count(*),
					count(*) FILTER (WHERE phase = 'completed')
				FROM device_command_attempts WHERE command_id = $1
			`, commandID).Scan(&commands, &attempts, &completed)
			return commands == 1 && attempts == 1 && completed == 1, err
		})
	})
}

func TestNewServerProcessRecoversKilledDispatchAfterLeaseExpiry(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		seedAuthTestAdmin(t, db)
		databaseURL := processTestDatabaseURL(t, db)
		redisURL := requireProcessRedisTestURL(t)
		crashed := startProcessServer(t, databaseURL, redisURL)
		defer crashed.kill(t)

		token := processLogin(t, crashed.baseURL)
		projectID := processCreateProject(t, crashed.baseURL, token, "")
		deviceID := processCreateSimulatorDevice(t, crashed.baseURL, token, projectID)
		processPatchSimulator(t, crashed.baseURL, token, "provider_accepted", 5000)
		status, envelope, err := processJSONRequest(crashed.baseURL, token, http.MethodPost, "/v1/device-commands",
			commandCreateBody(projectID, deviceID, "unlock", "killed-dispatch", `{}`))
		if err != nil || status != http.StatusCreated {
			t.Fatalf("create crash Command status=%d envelope=%+v err=%v", status, envelope, err)
		}
		var command processCommandResponse
		if err := json.Unmarshal(envelope.Data, &command); err != nil || command.ID == "" {
			t.Fatalf("decode crash Command: command=%+v err=%v", command, err)
		}
		waitForProcessState(t, 5*time.Second, func() (bool, error) {
			var phase string
			err := db.QueryRow(`SELECT phase FROM device_command_attempts WHERE command_id = $1`, command.ID).Scan(&phase)
			return phase == "dispatching", err
		})
		crashed.kill(t)

		recovered := startProcessServer(t, databaseURL, redisURL)
		defer recovered.kill(t)
		waitForProcessState(t, 20*time.Second, func() (bool, error) {
			var commandStatus, reasonCode, phase, outcome string
			var attemptCount int
			err := db.QueryRow(`
				SELECT c.status, c.reason_code, a.phase, a.outcome,
					(SELECT count(*) FROM device_command_attempts WHERE command_id = c.id)
				FROM device_commands c
				JOIN device_command_attempts a ON a.command_id = c.id
				WHERE c.id = $1
			`, command.ID).Scan(&commandStatus, &reasonCode, &phase, &outcome, &attemptCount)
			return commandStatus == "unknown" && reasonCode == "provider_delivery_unknown" &&
				phase == "completed" && outcome == "transport_error_after_send" && attemptCount == 1, err
		})
	})
}

func processTestDatabaseURL(t *testing.T, db *sql.DB) string {
	t.Helper()
	var searchPath string
	if err := db.QueryRow(`SHOW search_path`).Scan(&searchPath); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(requireServerMigrationTestDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", strings.Trim(searchPath, `"`))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func requireProcessRedisTestURL(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("MIGRATION_TEST_REDIS_URL"))
	if raw == "" {
		t.Skip("MIGRATION_TEST_REDIS_URL is not set")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "redis" && parsed.Scheme != "rediss") || parsed.Host == "" {
		t.Fatal("MIGRATION_TEST_REDIS_URL must be an absolute Redis URL")
	}
	return raw
}

func startProcessServer(t *testing.T, databaseURL, redisURL string) *processServer {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	command := exec.Command(executable, "-test.run=^TestMultiProcessServerHelper$")
	command.Env = append(os.Environ(),
		processServerHelperEnv+"=1",
		"DEVICE_PLATFORM_PROCESS_DATABASE_URL="+databaseURL,
		"DEVICE_PLATFORM_PROCESS_REDIS_URL="+redisURL,
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()
	type readyResult struct {
		address string
		output  string
	}
	ready := make(chan readyResult, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		var output strings.Builder
		for scanner.Scan() {
			if address := strings.TrimPrefix(scanner.Text(), "READY "); address != scanner.Text() {
				ready <- readyResult{address: address, output: output.String()}
				return
			}
			output.WriteString(scanner.Text())
			output.WriteByte('\n')
		}
		ready <- readyResult{output: output.String()}
	}()
	process := &processServer{command: command, done: done, stderr: stderr}
	select {
	case result := <-ready:
		if result.address == "" {
			process.kill(t)
			t.Fatalf("process server exited before ready: stdout=%s stderr=%s", result.output, stderr.String())
		}
		process.baseURL = "http://" + result.address
	case <-time.After(10 * time.Second):
		process.kill(t)
		t.Fatal("process server did not become ready")
	}
	t.Cleanup(func() { process.kill(t) })
	return process
}

func (p *processServer) kill(t *testing.T) {
	t.Helper()
	if p == nil || p.command == nil || p.command.Process == nil || p.done == nil {
		return
	}
	p.killOnce.Do(func() {
		_ = p.command.Process.Kill()
	})
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("process server did not exit: %s", p.stderr.String())
	}
}

func processLogin(t *testing.T, baseURL string) string {
	t.Helper()
	status, envelope, err := processJSONRequest(baseURL, "", http.MethodPost, "/v1/auth/login",
		`{"email":"`+authTestEmail+`","password":"`+authTestPassword+`"}`)
	if err != nil || status != http.StatusOK {
		t.Fatalf("process login status=%d envelope=%+v err=%v", status, envelope, err)
	}
	var data struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil || data.AccessToken == "" {
		t.Fatalf("decode process login: data=%+v err=%v", data, err)
	}
	return data.AccessToken
}

func processCreateProject(t *testing.T, baseURL, token, webhookURL string) string {
	t.Helper()
	body := map[string]any{"name": "Multi Process Project"}
	if webhookURL != "" {
		body["webhook_url"] = webhookURL
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	status, envelope, err := processJSONRequest(baseURL, token, http.MethodPost, "/v1/projects", string(rawBody))
	if err != nil || status != http.StatusCreated {
		t.Fatalf("create process Project status=%d envelope=%+v err=%v", status, envelope, err)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil || data.ID == "" {
		t.Fatalf("decode process Project: data=%+v err=%v", data, err)
	}
	return data.ID
}

func processCreateSimulatorDevice(t *testing.T, baseURL, token, projectID string) string {
	t.Helper()
	body := `{"project_id":"` + projectID + `","name":"Process Lock","device_type_code":"smart-lock","provider_code":"simulator"}`
	status, envelope, err := processJSONRequest(baseURL, token, http.MethodPost, "/v1/devices", body)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("create process Device status=%d envelope=%+v err=%v", status, envelope, err)
	}
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil || data.ID == "" {
		t.Fatalf("decode process Device: data=%+v err=%v", data, err)
	}
	return data.ID
}

func processPatchSimulator(t *testing.T, baseURL, token, outcome string, delayMS int) {
	t.Helper()
	body := fmt.Sprintf(`{"outcome":%q,"delay_ms":%d}`, outcome, delayMS)
	status, envelope, err := processJSONRequest(baseURL, token, http.MethodPatch, "/v1/simulator", body)
	if err != nil || status != http.StatusOK {
		t.Fatalf("patch process Simulator status=%d envelope=%+v err=%v", status, envelope, err)
	}
}

func processJSONRequest(baseURL, token, method, path, body string) (int, processEnvelope, error) {
	request, err := http.NewRequest(method, baseURL+path, strings.NewReader(body))
	if err != nil {
		return 0, processEnvelope{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return 0, processEnvelope{}, err
	}
	defer response.Body.Close()
	var envelope processEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return response.StatusCode, processEnvelope{}, err
	}
	if !envelope.Success {
		return response.StatusCode, envelope, fmt.Errorf("request failed with status %d", response.StatusCode)
	}
	return response.StatusCode, envelope, nil
}

func waitForProcessState(t *testing.T, timeout time.Duration, check func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		matched, err := check()
		if err == nil && matched {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process state was not reached: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
