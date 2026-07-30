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
	wantReclaimFields := []string{"WorkerID", "LeaseToken", "LeaseUntil"}
	if reclaimType.NumField() != len(wantReclaimFields) {
		t.Fatalf("ReclaimAttemptRequest fields = %d", reclaimType.NumField())
	}
	for index, name := range wantReclaimFields {
		if reclaimType.Field(index).Name != name {
			t.Fatalf("ReclaimAttemptRequest field %d = %s", index, reclaimType.Field(index).Name)
		}
	}

	claimType := reflect.TypeOf(ClaimCommandRequest{})
	if _, ok := claimType.FieldByName("RequestSummary"); !ok {
		t.Fatal("ClaimCommandRequest must persist the adapter request summary with the Attempt")
	}
	repositoryType := reflect.TypeOf((*CommandRepository)(nil)).Elem()
	for _, method := range []string{"CancelQueued", "ExpireQueued", "TransitionFromAttempt", "TransitionFromVerifiedEvidence"} {
		if _, ok := repositoryType.MethodByName(method); !ok {
			t.Fatalf("CommandRepository.%s is missing", method)
		}
	}
	if _, ok := repositoryType.MethodByName("TransitionStatus"); ok {
		t.Fatal("unfenced generic TransitionStatus must not be exposed")
	}

	verifiedType := reflect.TypeOf(VerifiedEvidenceTransitionRequest{})
	wantVerifiedFields := []string{
		"AttemptID",
		"RawMessageID",
		"RawMessageDeduplicationKey",
		"AttemptOutcome",
		"ResponseSummary",
		"Transition",
	}
	if verifiedType.NumField() != len(wantVerifiedFields) {
		t.Fatalf("VerifiedEvidenceTransitionRequest fields = %d", verifiedType.NumField())
	}
	for index, name := range wantVerifiedFields {
		if verifiedType.Field(index).Name != name {
			t.Fatalf("VerifiedEvidenceTransitionRequest field %d = %s", index, verifiedType.Field(index).Name)
		}
	}
	transitionMethod, ok := repositoryType.MethodByName("TransitionFromVerifiedEvidence")
	if !ok {
		t.Fatal("CommandRepository.TransitionFromVerifiedEvidence is missing")
	}
	wantVerifiedType := reflect.TypeOf(VerifiedEvidenceTransitionRequest{})
	if transitionMethod.Type.NumIn() != 3 || transitionMethod.Type.In(2) != wantVerifiedType {
		t.Fatalf("TransitionFromVerifiedEvidence must require typed evidence identity, got %v", transitionMethod.Type)
	}
}
