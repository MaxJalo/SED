package store

import (
	"errors"
	"testing"
)

func TestValidateAccessPermissions(t *testing.T) {
	for _, perm := range []string{"read", "write", "edit"} {
		if err := validateRoleGrant(perm); err != nil {
			t.Fatalf("expected %s to be valid, got %v", perm, err)
		}
	}
	for _, role := range []string{"author", "observer", "signer", "approver", "clerk"} {
		if err := validateRoleGrant(role); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected contextual/position role %s to be rejected, got %v", role, err)
		}
	}
	clean, err := validatePermissionSet([]string{"read", "write", "read", "edit"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clean) != 3 {
		t.Fatalf("expected 3 unique permissions, got %v", clean)
	}
}

func TestValidateTemplateStepsRejectsUnknownKind(t *testing.T) {
	err := validateTemplateSteps([]RouteTemplateStep{{
		OrderNo: 1, Kind: "registration", AssigneeRole: "clerk", DueDays: 1, Blocking: true,
	}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected invalid kind, got %v", err)
	}
	err = validateTemplateSteps([]RouteTemplateStep{{
		OrderNo: 1, Kind: "approval", AssigneeRole: "approver", DueDays: 2, Blocking: true,
	}})
	if err != nil {
		t.Fatalf("expected valid step, got %v", err)
	}
}

func TestMergeRolesKeepsUniqueOrder(t *testing.T) {
	got := MergeRoles([]string{"manager"}, []string{"read", "write", "read"})
	want := []string{"manager", "read", "write"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
}
