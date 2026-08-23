package adminauth

import "testing"

func TestRolePolicyIsExplicit(t *testing.T) {
	cases := []struct {
		role     string
		canRead  bool
		canWrite bool
	}{
		{"OWNER", true, true}, {"MANAGER", true, true},
		{"BARBER", true, false}, {"RECEPTIONIST", true, false}, {"UNKNOWN", false, false},
	}
	for _, tc := range cases {
		if CanRead(tc.role) != tc.canRead || CanWrite(tc.role) != tc.canWrite {
			t.Fatalf("unexpected policy for %s", tc.role)
		}
	}
	if !CanManageMembers("OWNER") {
		t.Fatal("OWNER must manage tenant members")
	}
	for _, role := range []string{"MANAGER", "BARBER", "RECEPTIONIST", "UNKNOWN"} {
		if CanManageMembers(role) {
			t.Fatalf("%s must not manage tenant members", role)
		}
	}
	for _, role := range []string{"OWNER", "MANAGER"} {
		if !CanManageTenantSettings(role) {
			t.Fatalf("%s must manage operational tenant settings", role)
		}
	}
	for _, role := range []string{"BARBER", "RECEPTIONIST", "UNKNOWN"} {
		if CanManageTenantSettings(role) {
			t.Fatalf("%s must be read-only for tenant settings", role)
		}
	}
}
