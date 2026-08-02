//go:build integration

package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	authorizationUserOneEmail    = "manager-one@example.test"
	authorizationUserOnePassword = "ManagerOne123!"
	authorizationUserTwoEmail    = "manager-two@example.test"
	authorizationUserTwoPassword = "ManagerTwo123!"
)

func TestMultiUserProjectAuthorizationHTTPContract(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		seedAuthTestAdmin(t, db)
		server := newPersistentHTTPTestServer(t, db, newDBAuthenticator(db, testJWTSecret))
		admin := bearerHeaders(loginAuthorizationTestUser(t, server, authTestEmail, authTestPassword))

		userOneID := createAuthorizationTestUser(t, server, admin, authorizationUserOneEmail, "Manager One", authorizationUserOnePassword)
		userTwoID := createAuthorizationTestUser(t, server, admin, authorizationUserTwoEmail, "Manager Two", authorizationUserTwoPassword)
		assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/users?page=1&page_size=10", "", admin), http.StatusOK, 1, 10, 3)
		userDetail := responseDataObject(t, assertEnvelope(t,
			doRequest(t, server, http.MethodGet, "/v1/users/"+userOneID, "", admin), http.StatusOK, true))
		if userDetail["email"] != authorizationUserOneEmail || userDetail["is_super_admin"] != false || userDetail["status"] != "active" {
			t.Fatalf("created User detail = %+v", userDetail)
		}
		if _, exists := userDetail["password"]; exists {
			t.Fatalf("User response disclosed password: %+v", userDetail)
		}

		userOneToken := loginAuthorizationTestUser(t, server, authorizationUserOneEmail, authorizationUserOnePassword)
		userOne := bearerHeaders(userOneToken)
		me := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/auth/me", "", userOne), http.StatusOK, true))
		if me["id"] != userOneID || me["email"] != authorizationUserOneEmail || me["display_name"] != "Manager One" ||
			me["is_super_admin"] != false || me["status"] != "active" || me["created_at"] == nil || me["updated_at"] == nil {
			t.Fatalf("ordinary User /auth/me = %+v", me)
		}
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/users", "", userOne), http.StatusForbidden, "forbidden")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/users",
			`{"email":"forbidden@example.test","display_name":"Forbidden User","password":"Forbidden123!"}`, userOne),
			http.StatusForbidden, "forbidden")

		projectOneID := createAuthorizationTestProject(t, server, admin, "Managed Project One", userOneID, true)
		projectTwoID := createAuthorizationTestProject(t, server, admin, "Managed Project Two", userTwoID, true)
		adminProject := responseDataObject(t, assertEnvelope(t,
			doRequest(t, server, http.MethodGet, "/v1/projects/"+projectOneID, "", admin), http.StatusOK, true))
		for _, key := range []string{"webhook_url", "webhook_configured", "ip_whitelist"} {
			if _, exists := adminProject[key]; !exists {
				t.Fatalf("super administrator Project DTO omitted %s: %+v", key, adminProject)
			}
		}
		projectList := assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/projects", "", userOne), http.StatusOK, 1, 20, 1)
		projectItems := responseDataObject(t, projectList)["items"].([]interface{})
		projectOne := projectItems[0].(map[string]interface{})
		if projectOne["id"] != projectOneID || projectOne["manager_user_id"] != userOneID {
			t.Fatalf("ordinary User Project DTO = %+v", projectOne)
		}
		for _, key := range []string{"webhook_url", "webhook_configured", "ip_whitelist"} {
			if _, exists := projectOne[key]; exists {
				t.Fatalf("ordinary User Project DTO exposed %s: %+v", key, projectOne)
			}
		}
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/projects/"+projectTwoID, "", userOne), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/projects?manager_user_id="+userTwoID, "", userOne), http.StatusForbidden, "forbidden")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/projects",
			`{"name":"Forbidden Project","manager_user_id":"`+userOneID+`"}`, userOne), http.StatusForbidden, "forbidden")

		renamed := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPatch, "/v1/projects/"+projectOneID,
			`{"name":"Managed Project One Renamed"}`, userOne), http.StatusOK, true))
		if renamed["name"] != "Managed Project One Renamed" {
			t.Fatalf("ordinary User Project rename = %+v", renamed)
		}
		for _, key := range []string{"webhook_url", "webhook_configured", "ip_whitelist"} {
			if _, exists := renamed[key]; exists {
				t.Fatalf("ordinary User Project update exposed %s: %+v", key, renamed)
			}
		}
		for _, request := range []struct {
			method string
			path   string
			body   string
		}{
			{http.MethodPatch, "/v1/projects/" + projectOneID, `{"webhook_url":"https://hooks.example.test/changed"}`},
			{http.MethodPatch, "/v1/projects/" + projectOneID, `{"ip_whitelist":["192.0.2.0/24"]}`},
			{http.MethodPost, "/v1/projects/" + projectOneID + "/transfer", `{"manager_user_id":"` + userTwoID + `"}`},
			{http.MethodPost, "/v1/projects/" + projectOneID + "/api-key/rotate", `{}`},
			{http.MethodPost, "/v1/projects/" + projectOneID + "/webhook-secret/rotate", `{}`},
			{http.MethodGet, "/v1/simulator", ""},
		} {
			assertErrorCode(t, doRequest(t, server, request.method, request.path, request.body, userOne), http.StatusForbidden, "forbidden")
		}

		deviceOneID := createAuthorizationTestDevice(t, server, userOne, projectOneID, "Manager One Lock")
		deviceTwoID := createAuthorizationTestDevice(t, server, admin, projectTwoID, "Manager Two Lock")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/devices",
			`{"project_id":"`+projectTwoID+`","name":"Foreign Lock","device_type_code":"smart-lock","provider_code":"simulator","provider_profile":"simulator-v1"}`,
			userOne), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/devices?project_id="+projectTwoID, "", userOne), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/devices/"+deviceTwoID, "", userOne), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodPatch, "/v1/devices/"+deviceTwoID, `{"name":"Hidden"}`, userOne), http.StatusNotFound, "not_found")
		updatedDevice := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPatch, "/v1/devices/"+deviceOneID,
			`{"name":"Manager One Lock Renamed"}`, userOne), http.StatusOK, true))
		if updatedDevice["name"] != "Manager One Lock Renamed" {
			t.Fatalf("ordinary User Device update = %+v", updatedDevice)
		}

		commandOne := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(projectOneID, deviceOneID, "query_status", "manager-one-command", `{}`), userOne), http.StatusCreated, true))
		commandOneID := requiredStringField(t, commandOne, "id")
		commandTwo := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(projectTwoID, deviceTwoID, "query_status", "manager-two-command", `{}`), admin), http.StatusCreated, true))
		commandTwoID := requiredStringField(t, commandTwo, "id")
		assertEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/device-commands/"+commandOneID, "", userOne), http.StatusOK, true)
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/device-commands/"+commandTwoID, "", userOne), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/device-commands",
			commandCreateBody(projectTwoID, deviceTwoID, "query_status", "foreign-command", `{}`), userOne), http.StatusNotFound, "not_found")

		var eventOneID, eventTwoID, auditTwoID, deliveryOneID string
		var projectOneEvents, projectOneAudits int
		if err := db.QueryRow(`SELECT id::text FROM device_events WHERE project_id = $1 AND device_id = $2 AND event_type = 'device.created'`, projectOneID, deviceOneID).Scan(&eventOneID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id::text FROM device_events WHERE project_id = $1 AND device_id = $2 AND event_type = 'device.created'`, projectTwoID, deviceTwoID).Scan(&eventTwoID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id::text FROM audit_logs WHERE project_id = $1 ORDER BY occurred_at LIMIT 1`, projectTwoID).Scan(&auditTwoID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id::text FROM webhook_deliveries WHERE project_id = $1 AND event_id = $2`, projectOneID, eventOneID).Scan(&deliveryOneID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM device_events WHERE project_id = $1`, projectOneID).Scan(&projectOneEvents); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE project_id = $1`, projectOneID).Scan(&projectOneAudits); err != nil {
			t.Fatal(err)
		}
		assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/events?project_id="+projectOneID, "", userOne), http.StatusOK, 1, 20, projectOneEvents)
		assertEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/events/"+eventOneID, "", userOne), http.StatusOK, true)
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/events?project_id="+projectTwoID, "", userOne), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/events/"+eventTwoID, "", userOne), http.StatusNotFound, "not_found")
		assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/audit-logs?project_id="+projectOneID, "", userOne), http.StatusOK, 1, 20, projectOneAudits)
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/audit-logs?project_id="+projectTwoID, "", userOne), http.StatusNotFound, "not_found")
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/audit-logs/"+auditTwoID, "", userOne), http.StatusNotFound, "not_found")

		if _, err := db.Exec(`UPDATE webhook_deliveries SET status = 'dead', attempt_count = 1, next_attempt_at = NULL WHERE id = $1`, deliveryOneID); err != nil {
			t.Fatal(err)
		}
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/webhook-deliveries/"+deliveryOneID+"/resend", "", userOne), http.StatusForbidden, "forbidden")

		assertErrorCode(t, doRequest(t, server, http.MethodPatch, "/v1/users/"+userOneID, `{"status":"disabled"}`, admin),
			http.StatusConflict, "user_has_managed_projects")
		transferred := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPost, "/v1/projects/"+projectOneID+"/transfer",
			`{"manager_user_id":"`+userTwoID+`"}`, admin), http.StatusOK, true))
		if transferred["manager_user_id"] != userTwoID {
			t.Fatalf("transferred Project = %+v", transferred)
		}
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/projects/"+projectOneID, "", userOne), http.StatusNotFound, "not_found")

		disabled := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPatch, "/v1/users/"+userOneID,
			`{"status":"disabled"}`, admin), http.StatusOK, true))
		if disabled["status"] != "disabled" {
			t.Fatalf("disabled User = %+v", disabled)
		}
		assertErrorCode(t, doRequest(t, server, http.MethodGet, "/v1/auth/me", "", bearerHeaders(userOneToken)), http.StatusUnauthorized, "unauthorized")
		assertErrorCode(t, doRequest(t, server, http.MethodPost, "/v1/auth/login",
			`{"email":"`+authorizationUserOneEmail+`","password":"`+authorizationUserOnePassword+`"}`, nil),
			http.StatusUnauthorized, "invalid_credentials")

		assertEnvelope(t, doRequest(t, server, http.MethodPatch, "/v1/users/"+userOneID, `{"status":"active"}`, admin), http.StatusOK, true)
		reenabled := bearerHeaders(loginAuthorizationTestUser(t, server, authorizationUserOneEmail, authorizationUserOnePassword))
		assertPaginatedEnvelope(t, doRequest(t, server, http.MethodGet, "/v1/projects", "", reenabled), http.StatusOK, 1, 20, 0)

		for _, audit := range []struct {
			action      string
			resourceID  string
			actorUserID string
		}{
			{"user.created", userOneID, authTestAdminID},
			{"project.transferred", projectOneID, authTestAdminID},
			{"device.created", deviceOneID, userOneID},
			{"command.created", commandOneID, userOneID},
			{"user.status_changed", userOneID, authTestAdminID},
		} {
			var actorType, actorUserID string
			if err := db.QueryRow(`
				SELECT actor_type, actor_user_id::text
				FROM audit_logs
				WHERE action = $1 AND resource_id = $2
				ORDER BY occurred_at DESC LIMIT 1
			`, audit.action, audit.resourceID).Scan(&actorType, &actorUserID); err != nil {
				t.Fatalf("read %s Audit: %v", audit.action, err)
			}
			if actorType != "user" || actorUserID != audit.actorUserID {
				t.Fatalf("%s Audit actor = %s/%s, want user/%s", audit.action, actorType, actorUserID, audit.actorUserID)
			}
		}
	})
}

func TestProjectTransferAndUserDisableRacePreservesActiveManager(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		seedAuthTestAdmin(t, db)
		server := newPersistentHTTPTestServer(t, db, newDBAuthenticator(db, testJWTSecret))
		adminToken := loginAuthorizationTestUser(t, server, authTestEmail, authTestPassword)
		admin := bearerHeaders(adminToken)

		for iteration := 1; iteration <= 12; iteration++ {
			currentManagerID := fmt.Sprintf("71000000-0000-0000-0000-%012d", iteration)
			targetManagerID := fmt.Sprintf("72000000-0000-0000-0000-%012d", iteration)
			if _, err := db.Exec(`
				INSERT INTO users (id, email, password_hash, display_name, is_super_admin, status)
				VALUES
					($1, $2, 'unused', 'Current Manager', false, 'active'),
					($3, $4, 'unused', 'Target Manager', false, 'active')
			`, currentManagerID, fmt.Sprintf("race-current-%d@example.test", iteration),
				targetManagerID, fmt.Sprintf("race-target-%d@example.test", iteration)); err != nil {
				t.Fatal(err)
			}
			projectID := createAuthorizationTestProject(t, server, admin, fmt.Sprintf("Race Project %d", iteration), currentManagerID, false)

			start := make(chan struct{})
			responses := make(chan *httptest.ResponseRecorder, 2)
			var workers sync.WaitGroup
			for _, request := range []struct {
				method string
				path   string
				body   string
			}{
				{http.MethodPost, "/v1/projects/" + projectID + "/transfer", `{"manager_user_id":"` + targetManagerID + `"}`},
				{http.MethodPatch, "/v1/users/" + targetManagerID, `{"status":"disabled"}`},
			} {
				request := request
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					recorder := httptest.NewRecorder()
					httpRequest := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
					httpRequest.Header.Set("Authorization", "Bearer "+adminToken)
					httpRequest.Header.Set("Content-Type", "application/json")
					server.ServeHTTP(recorder, httpRequest)
					responses <- recorder
				}()
			}
			close(start)
			workers.Wait()
			close(responses)

			statuses := map[int]int{}
			for response := range responses {
				statuses[response.Code]++
				if response.Code == http.StatusConflict {
					var body jsonResponse
					decodeBody(t, response.Body, &body)
					if body.ErrorCode != "project_manager_inactive" && body.ErrorCode != "user_has_managed_projects" {
						t.Fatalf("iteration %d conflict code = %q", iteration, body.ErrorCode)
					}
				}
			}
			if statuses[http.StatusOK] != 1 || statuses[http.StatusConflict] != 1 {
				t.Fatalf("iteration %d concurrent statuses = %+v, want one 200 and one 409", iteration, statuses)
			}

			var managerStatus string
			if err := db.QueryRow(`
				SELECT users.status
				FROM projects JOIN users ON users.id = projects.manager_user_id
				WHERE projects.id = $1
			`, projectID).Scan(&managerStatus); err != nil {
				t.Fatal(err)
			}
			if managerStatus != "active" {
				t.Fatalf("iteration %d committed Project manager status = %q", iteration, managerStatus)
			}
		}
	})
}

func createAuthorizationTestUser(t *testing.T, server http.Handler, admin map[string]string, email, displayName, password string) string {
	t.Helper()
	response := doRequest(t, server, http.MethodPost, "/v1/users",
		`{"email":"`+email+`","display_name":"`+displayName+`","password":"`+password+`"}`, admin)
	return requiredStringField(t, responseDataObject(t, assertEnvelope(t, response, http.StatusCreated, true)), "id")
}

func createAuthorizationTestProject(t *testing.T, server http.Handler, admin map[string]string, name, managerUserID string, webhook bool) string {
	t.Helper()
	body := `{"name":"` + name + `","manager_user_id":"` + managerUserID + `"`
	if webhook {
		body += `,"webhook_url":"https://hooks.example.test/authorization"`
	}
	body += `}`
	response := doRequest(t, server, http.MethodPost, "/v1/projects", body, admin)
	return requiredStringField(t, responseDataObject(t, assertEnvelope(t, response, http.StatusCreated, true)), "id")
}

func createAuthorizationTestDevice(t *testing.T, server http.Handler, headers map[string]string, projectID, name string) string {
	t.Helper()
	response := doRequest(t, server, http.MethodPost, "/v1/devices",
		`{"project_id":"`+projectID+`","name":"`+name+`","device_type_code":"smart-lock","provider_code":"simulator","provider_profile":"simulator-v1"}`,
		headers)
	return requiredStringField(t, responseDataObject(t, assertEnvelope(t, response, http.StatusCreated, true)), "id")
}

func loginAuthorizationTestUser(t *testing.T, server http.Handler, email, password string) string {
	t.Helper()
	response := doRequest(t, server, http.MethodPost, "/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`, nil)
	return dataFieldString(t, assertEnvelope(t, response, http.StatusOK, true), "access_token")
}

func bearerHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}
