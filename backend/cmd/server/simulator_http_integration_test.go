//go:build integration

package main

import (
	"database/sql"
	"net/http"
	"sync"
	"testing"
)

func TestPersistentSimulatorHTTPContractAuditAndRestart(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		admin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, server)}

		initial := responseDataObject(t, assertEnvelope(t,
			doRequest(t, server, http.MethodGet, "/v1/simulator", "", admin), http.StatusOK, true))
		if initial["outcome"] != "provider_accepted" || initial["delay_ms"] != float64(0) || initial["version"] != float64(1) {
			t.Fatalf("initial Simulator config=%+v", initial)
		}
		for _, request := range []struct {
			method string
			path   string
			body   string
			status int
			code   string
		}{
			{http.MethodPost, "/v1/simulator", `{"outcome":"provider_accepted","delay_ms":0}`, http.StatusMethodNotAllowed, "method_not_allowed"},
			{http.MethodGet, "/v1/simulator?mode=normal", "", http.StatusBadRequest, "invalid_request"},
			{http.MethodGet, "/v1/simulator/gateway", "", http.StatusNotFound, "not_found"},
			{http.MethodPatch, "/v1/simulator", `{"outcome":"provider_accepted"}`, http.StatusBadRequest, "invalid_request"},
			{http.MethodPatch, "/v1/simulator", `{"outcome":"future","delay_ms":0}`, http.StatusBadRequest, "invalid_request"},
			{http.MethodPatch, "/v1/simulator", `{"outcome":"provider_accepted","delay_ms":60001}`, http.StatusBadRequest, "invalid_request"},
			{http.MethodPatch, "/v1/simulator", `{"outcome":"provider_accepted","delay_ms":0,"mode":"normal"}`, http.StatusBadRequest, "invalid_request"},
		} {
			assertErrorCode(t, doRequest(t, server, request.method, request.path, request.body, admin), request.status, request.code)
		}

		updated := responseDataObject(t, assertEnvelope(t, doRequest(t, server, http.MethodPatch, "/v1/simulator",
			`{"outcome":"provider_rejected","delay_ms":125}`, admin), http.StatusOK, true))
		if updated["outcome"] != "provider_rejected" || updated["delay_ms"] != float64(125) || updated["version"] != float64(2) {
			t.Fatalf("updated Simulator config=%+v", updated)
		}
		var action, actorType, actorID, resourceType, requestID string
		var projectID, resourceID sql.NullString
		var metadata string
		if err := db.QueryRow(`
			SELECT action, actor_type, actor_id, project_id, resource_type, resource_id, request_id, metadata::text
			FROM audit_logs WHERE action = 'simulator.updated'
		`).Scan(&action, &actorType, &actorID, &projectID, &resourceType, &resourceID, &requestID, &metadata); err != nil {
			t.Fatal(err)
		}
		if action != "simulator.updated" || actorType != "admin" || actorID == "" || resourceType != "simulator" ||
			projectID.Valid || resourceID.Valid || requestID == "" || metadata == "" {
			t.Fatalf("Simulator Audit=%s/%s/%s project=%v resource=%s/%v request=%q metadata=%s",
				action, actorType, actorID, projectID, resourceType, resourceID, requestID, metadata)
		}

		restarted := newProjectHTTPTestServer(t, db)
		restartedAdmin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, restarted)}
		afterRestart := responseDataObject(t, assertEnvelope(t,
			doRequest(t, restarted, http.MethodGet, "/v1/simulator", "", restartedAdmin), http.StatusOK, true))
		if afterRestart["outcome"] != "provider_rejected" || afterRestart["version"] != float64(2) {
			t.Fatalf("restarted Simulator config=%+v", afterRestart)
		}
	})
}

func TestPersistentSimulatorConcurrentUpdatesAndAuditRollback(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		server := newProjectHTTPTestServer(t, db)
		admin := map[string]string{"Authorization": "Bearer " + loginProjectHTTPTestAdmin(t, server)}
		start := make(chan struct{})
		statuses := make(chan int, 2)
		var workers sync.WaitGroup
		for _, body := range []string{
			`{"outcome":"transport_error_after_send","delay_ms":1}`,
			`{"outcome":"invalid_response","delay_ms":2}`,
		} {
			body := body
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				statuses <- doRequest(t, server, http.MethodPatch, "/v1/simulator", body, admin).Code
			}()
		}
		close(start)
		workers.Wait()
		close(statuses)
		for status := range statuses {
			if status != http.StatusOK {
				t.Fatalf("concurrent Simulator update status=%d", status)
			}
		}
		var version, audits int
		if err := db.QueryRow(`SELECT version FROM simulator_config WHERE singleton`).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'simulator.updated'`).Scan(&audits); err != nil {
			t.Fatal(err)
		}
		if version != 3 || audits != 2 {
			t.Fatalf("concurrent Simulator version/audits=%d/%d, want 3/2", version, audits)
		}

		if _, err := db.Exec(`ALTER TABLE audit_logs ADD CONSTRAINT reject_simulator_audit CHECK (action <> 'simulator.updated') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		failed := doRequest(t, server, http.MethodPatch, "/v1/simulator", `{"outcome":"provider_accepted","delay_ms":3}`, admin)
		assertEnvelope(t, failed, http.StatusInternalServerError, false)
		var afterVersion int
		if err := db.QueryRow(`SELECT version FROM simulator_config WHERE singleton`).Scan(&afterVersion); err != nil {
			t.Fatal(err)
		}
		if afterVersion != version {
			t.Fatalf("Simulator config committed without Audit: version=%d, want %d", afterVersion, version)
		}
	})
}
