package repository

import (
	"reflect"
	"testing"
)

func TestStoreExposesOnlyReadRepositoriesOutsideTransaction(t *testing.T) {
	storeType := reflect.TypeOf((*Store)(nil)).Elem()
	users, ok := storeType.MethodByName("Users")
	if !ok {
		t.Fatal("Store.Users is missing")
	}
	if users.Type.NumOut() != 1 || users.Type.Out(0) != reflect.TypeOf((*UserQueries)(nil)).Elem() {
		t.Fatalf("Store.Users returns %v, want UserQueries", users.Type)
	}
	if _, ok := users.Type.Out(0).MethodByName("Create"); ok {
		t.Fatal("Store must not expose User mutation outside WithinTransaction")
	}
	commands, ok := storeType.MethodByName("Commands")
	if !ok {
		t.Fatal("Store.Commands is missing")
	}
	if _, ok := commands.Type.Out(0).MethodByName("CancelQueued"); ok {
		t.Fatal("Store must not expose Command mutation outside WithinTransaction")
	}
}

func TestCommandRepositoryContractFencesOwnershipAndAttemptIdentity(t *testing.T) {
	reclaimType := reflect.TypeOf(ReclaimAttemptRequest{})
	wantReclaimFields := []string{"WorkerID", "LeaseToken", "LeaseDuration"}
	if reclaimType.NumField() != len(wantReclaimFields) {
		t.Fatalf("ReclaimAttemptRequest fields = %d", reclaimType.NumField())
	}
	for index, name := range wantReclaimFields {
		if reclaimType.Field(index).Name != name {
			t.Fatalf("ReclaimAttemptRequest field %d = %s", index, reclaimType.Field(index).Name)
		}
	}

	claimType := reflect.TypeOf(ClaimCommandRequest{})
	if _, ok := claimType.FieldByName("LeaseDuration"); !ok {
		t.Fatal("ClaimCommandRequest must express a database-clock lease duration")
	}
	if _, ok := claimType.FieldByName("LeaseUntil"); ok {
		t.Fatal("ClaimCommandRequest must not accept an absolute caller-clock lease")
	}
	if _, ok := claimType.FieldByName("ClaimedAt"); ok {
		t.Fatal("ClaimCommandRequest must not accept a caller-clock claim time")
	}
	if _, ok := claimType.FieldByName("RequestSummary"); !ok {
		t.Fatal("ClaimCommandRequest must persist the adapter request summary with the Attempt")
	}
	repositoryType := reflect.TypeOf((*CommandRepository)(nil)).Elem()
	for _, method := range []string{
		"CancelQueued",
		"ExpireQueued",
		"ExpireResultObservation",
		"RecoverExpiredDispatching",
		"UpdateEvidenceFromAttempt",
		"TransitionFromAttempt",
		"UpdateProviderAcceptanceFromVerifiedMessage",
	} {
		if _, ok := repositoryType.MethodByName(method); !ok {
			t.Fatalf("CommandRepository.%s is missing", method)
		}
	}
	if _, ok := repositoryType.MethodByName("TransitionStatus"); ok {
		t.Fatal("unfenced generic TransitionStatus must not be exposed")
	}

	verifiedType := reflect.TypeOf(VerifiedEvidenceUpdateRequest{})
	wantVerifiedFields := []string{
		"AttemptID",
		"RawMessageID",
		"RawMessageDeduplicationKey",
		"AttemptOutcome",
		"ResponseSummary",
		"ExpectedStatus",
	}
	if verifiedType.NumField() != len(wantVerifiedFields) {
		t.Fatalf("VerifiedEvidenceUpdateRequest fields = %d", verifiedType.NumField())
	}
	for index, name := range wantVerifiedFields {
		if verifiedType.Field(index).Name != name {
			t.Fatalf("VerifiedEvidenceUpdateRequest field %d = %s", index, verifiedType.Field(index).Name)
		}
	}
	transitionMethod, ok := repositoryType.MethodByName("UpdateProviderAcceptanceFromVerifiedMessage")
	if !ok {
		t.Fatal("CommandRepository.UpdateProviderAcceptanceFromVerifiedMessage is missing")
	}
	wantVerifiedType := reflect.TypeOf(VerifiedEvidenceUpdateRequest{})
	if transitionMethod.Type.NumIn() != 3 || transitionMethod.Type.In(2) != wantVerifiedType {
		t.Fatalf("UpdateProviderAcceptanceFromVerifiedMessage must require typed evidence identity, got %v", transitionMethod.Type)
	}
}

func TestWebhookRepositoryContractUsesDatabaseClockAndDerivedState(t *testing.T) {
	createType := reflect.TypeOf(CreateWebhookDeliveryRequest{})
	wantCreateFields := []string{"ID", "EventID", "RawBody"}
	if createType.NumField() != len(wantCreateFields) {
		t.Fatalf("CreateWebhookDeliveryRequest fields = %d", createType.NumField())
	}
	for index, name := range wantCreateFields {
		if createType.Field(index).Name != name {
			t.Fatalf("CreateWebhookDeliveryRequest field %d = %s", index, createType.Field(index).Name)
		}
	}
	replayType := reflect.TypeOf(CreateWebhookReplayRequest{})
	wantReplayFields := []string{"ID", "ReplayOfDeliveryID"}
	if replayType.NumField() != len(wantReplayFields) {
		t.Fatalf("CreateWebhookReplayRequest fields = %d", replayType.NumField())
	}
	for index, name := range wantReplayFields {
		if replayType.Field(index).Name != name {
			t.Fatalf("CreateWebhookReplayRequest field %d = %s", index, replayType.Field(index).Name)
		}
	}

	claimType := reflect.TypeOf(ClaimWebhookRequest{})
	wantClaimFields := []string{"WorkerID", "LeaseToken", "LeaseDuration", "MaxAttempts"}
	if claimType.NumField() != len(wantClaimFields) {
		t.Fatalf("ClaimWebhookRequest fields = %d", claimType.NumField())
	}
	for index, name := range wantClaimFields {
		if claimType.Field(index).Name != name {
			t.Fatalf("ClaimWebhookRequest field %d = %s", index, claimType.Field(index).Name)
		}
	}
	for _, forbidden := range []string{"LeaseUntil", "StartedAt", "Timestamp"} {
		if _, ok := claimType.FieldByName(forbidden); ok {
			t.Fatalf("ClaimWebhookRequest must not accept caller-clock field %s", forbidden)
		}
	}

	completeType := reflect.TypeOf(CompleteWebhookAttemptRequest{})
	for _, forbidden := range []string{"CompletedAt", "NextStatus", "NextAttemptAt"} {
		if _, ok := completeType.FieldByName(forbidden); ok {
			t.Fatalf("CompleteWebhookAttemptRequest must not accept caller-derived field %s", forbidden)
		}
	}
	for _, required := range []string{"RetryDelay", "MaxAttempts"} {
		if _, ok := completeType.FieldByName(required); !ok {
			t.Fatalf("CompleteWebhookAttemptRequest is missing %s", required)
		}
	}
	recoverType := reflect.TypeOf(RecoverExpiredWebhookRequest{})
	if _, ok := recoverType.FieldByName("RetrySchedule"); !ok {
		t.Fatal("RecoverExpiredWebhookRequest must provide the complete retry schedule")
	}
	if _, ok := recoverType.FieldByName("RetryDelay"); ok {
		t.Fatal("RecoverExpiredWebhookRequest must not require a delay chosen before the expired Delivery is discovered")
	}
	repositoryType := reflect.TypeOf((*WebhookRepository)(nil)).Elem()
	for _, method := range []string{"ExhaustRetryBudget", "RecoverNextExpiredSending"} {
		if _, ok := repositoryType.MethodByName(method); !ok {
			t.Fatalf("WebhookRepository.%s is missing", method)
		}
	}
}
