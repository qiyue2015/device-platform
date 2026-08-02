package access

import "testing"

func TestScopeValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scope   Scope
		wantErr bool
	}{
		{name: "super administrator", scope: SuperAdmin("user-1")},
		{name: "ordinary User", scope: User("user-1")},
		{name: "Project credential", scope: Project("project-1")},
		{name: "super administrator without User", scope: SuperAdmin(""), wantErr: true},
		{name: "ordinary User without User", scope: User("   "), wantErr: true},
		{name: "Project credential without Project", scope: Project(""), wantErr: true},
		{
			name:    "human scope with Project",
			scope:   Scope{Kind: ScopeUser, UserID: "user-1", ProjectID: "project-1"},
			wantErr: true,
		},
		{
			name:    "Project scope with User",
			scope:   Scope{Kind: ScopeProject, UserID: "user-1", ProjectID: "project-1"},
			wantErr: true,
		},
		{name: "unknown kind", scope: Scope{Kind: "unknown"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.scope.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
