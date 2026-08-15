package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"io.astrasync/control-plane/auth"
)

func TestAdminCommandRunRejectsUnknownOperation(t *testing.T) {
	command := adminCommand{operation: "nope", env: func(string) string { return "" }, stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{}, clock: func() time.Time { return time.Unix(0, 0) }}
	if err := command.run(context.Background(), []string{"-database-url", "ignored"}); err == nil ||
		!strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("expected unknown operation error, got %v", err)
	}
}

func TestBootstrapTenantValidatesBeforeDatabase(t *testing.T) {
	tests := []struct {
		name    string
		flags   []string
		wantSub string
	}{
		{name: "missing confirm", flags: []string{}, wantSub: "requires -confirm"},
		{name: "missing tenant id",
			flags:   []string{"-confirm"},
			wantSub: "tenant-id must be a canonical UUID"},
		{name: "missing namespace",
			flags:   []string{"-confirm", "-tenant-id", "00000000-0000-4000-8000-000000000001"},
			wantSub: "namespace is required"},
		{name: "missing identity",
			flags:   []string{"-confirm", "-tenant-id", "00000000-0000-4000-8000-000000000001", "-namespace", "team-a"},
			wantSub: "oidc-issuer and oidc-subject are required"},
		{name: "invalid issuer",
			flags: []string{"-confirm", "-tenant-id", "00000000-0000-4000-8000-000000000001",
				"-namespace", "team-a", "-oidc-issuer", "http://insecure", "-oidc-subject", "user"},
			wantSub: "OIDC issuer must be an HTTPS URL"},
		{name: "invalid namespace case",
			flags: []string{"-confirm", "-tenant-id", "00000000-0000-4000-8000-000000000001",
				"-namespace", "Team-A", "-oidc-issuer", "https://issuer", "-oidc-subject", "user"},
			wantSub: "tenant namespace"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command := newTestCommand(opBootstrapTenant)
			args := append([]string{"-database-url", "ignored"}, tc.flags...)
			err := command.run(context.Background(), args)
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected error containing %q, got %v", tc.wantSub, err)
			}
		})
	}
}

func TestBootstrapPlatformAdminRejectsPlainIssuer(t *testing.T) {
	command := newTestCommand(opBootstrapPlatformAdmin)
	err := command.run(context.Background(), []string{"-database-url", "ignored", "-confirm",
		"-oidc-issuer", "http://issuer", "-oidc-subject", "admin"})
	if err == nil || !strings.Contains(err.Error(), "OIDC issuer must be an HTTPS URL") {
		t.Fatalf("expected HTTPS enforcement, got %v", err)
	}
}

func TestDisablePrincipalValidatesUUID(t *testing.T) {
	command := newTestCommand(opDisablePrincipal)
	err := command.run(context.Background(),
		[]string{"-database-url", "ignored", "-confirm", "-principal-id", "garbage"})
	if err == nil || !strings.Contains(err.Error(), "principal-id must be a canonical UUID") {
		t.Fatalf("expected UUID validation error, got %v", err)
	}
}

func TestSuspendTenantValidatesUUID(t *testing.T) {
	command := newTestCommand(opSuspendTenant)
	err := command.run(context.Background(),
		[]string{"-database-url", "ignored", "-confirm", "-tenant-id", "not-uuid"})
	if err == nil || !strings.Contains(err.Error(), "tenant-id must be a canonical UUID") {
		t.Fatalf("expected UUID validation error, got %v", err)
	}
}

func TestShowRevisionRejectsMissingTenantID(t *testing.T) {
	command := newTestCommand(opShowRevision)
	err := command.run(context.Background(), []string{"-database-url", "ignored"})
	if err == nil || !strings.Contains(err.Error(), "tenant-id must be a canonical UUID") {
		t.Fatalf("expected UUID validation error, got %v", err)
	}
}

func TestAuthValidationHelpers(t *testing.T) {
	if err := auth.ValidateTenant("team-a", "Team A"); err != nil {
		t.Fatalf("valid tenant rejected: %v", err)
	}
	if err := auth.ValidateTenant("Team-A", "Team A"); err == nil {
		t.Fatalf("expected namespace rejection for uppercase")
	}
	if err := auth.ValidateTenant("tenant_with_underscore", ""); err == nil {
		t.Fatalf("expected underscore namespace rejection")
	}
	if err := auth.ValidateExternalIdentity(auth.ExternalIdentity{Issuer: "https://issuer", Subject: "user"}); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}
	if err := auth.ValidateExternalIdentity(auth.ExternalIdentity{Issuer: "http://issuer", Subject: "user"}); err == nil {
		t.Fatalf("expected plain HTTP issuer rejection")
	}
	if err := auth.ValidateExternalIdentity(auth.ExternalIdentity{Issuer: "https://issuer", Subject: ""}); err == nil {
		t.Fatalf("expected blank subject rejection")
	}
}

func TestSortedTenantRolesDeterministic(t *testing.T) {
	roles := auth.SortedTenantRoles()
	if len(roles) != 4 {
		t.Fatalf("expected four built-in tenant roles, got %d", len(roles))
	}
	if string(roles[0]) != string(auth.RoleTenantAdmin) {
		t.Fatalf("expected stable sorted ordering starting with tenant_admin, got %v", roles)
	}
}

func newTestCommand(op adminOperation) adminCommand {
	return adminCommand{operation: op, env: func(string) string { return "" }, stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{}, clock: func() time.Time { return time.Unix(0, 0) }}
}
