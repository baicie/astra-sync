// Command astra-auth-admin is an explicit offline operator utility for
// bootstrapping and inspecting Slice 18 authentication state. It performs one
// idempotent operation against the authentication PostgreSQL repository and
// exits. Production API surface must never reuse these flows; they exist so
// that operators can perform first-time setup, verify bootstrap, and audit
// authorization revisions without exposing write endpoints in the API Server.
//
// All operations require explicit confirmation flags and never log secret
// material. Database URLs, OIDC issuer/subject, and tenant identifiers come
// from environment variables or explicit flags; they are not prompted.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/auth"
	authpostgres "io.astrasync/control-plane/auth/postgres"
)

type adminOperation string

const (
	opBootstrapTenant        adminOperation = "bootstrap-tenant"
	opBootstrapPlatformAdmin adminOperation = "bootstrap-platform-admin"
	opDisablePrincipal       adminOperation = "disable-principal"
	opShowTenant             adminOperation = "show-tenant"
	opSuspendTenant          adminOperation = "suspend-tenant"
	opReactivateTenant       adminOperation = "reactivate-tenant"
	opRevokeSession          adminOperation = "revoke-session"
	opShowRevision           adminOperation = "show-revision"
)

type adminCommand struct {
	operation adminOperation
	env       envLookup
	stdout    io.Writer
	stderr    io.Writer
	clock     func() time.Time
	uid       func() string
}

type envLookup func(string) string

func defaultEnv(key string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return ""
}

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return
	}
	operation := adminOperation(strings.TrimSpace(os.Args[1]))
	switch operation {
	case opBootstrapTenant, opBootstrapPlatformAdmin, opDisablePrincipal, opShowTenant,
		opSuspendTenant, opReactivateTenant, opRevokeSession, opShowRevision:
	default:
		fmt.Fprintf(os.Stderr, "astra-auth-admin: unknown operation %q\n\n", operation)
		printUsage(os.Stderr)
		os.Exit(2)
	}
	command := adminCommand{
		operation: operation,
		env:       defaultEnv,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		clock:     func() time.Time { return time.Now().UTC() },
		uid:       uuid.NewString,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := command.run(ctx, os.Args[2:]); err != nil {
		fmt.Fprintf(command.stderr, "astra-auth-admin: %s: %v\n", operation, err)
		os.Exit(1)
	}
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "astra-auth-admin performs offline administrative operations on the control-plane authentication store.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  astra-auth-admin <operation> [flags]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Operations:")
	fmt.Fprintln(out, "  bootstrap-tenant        Create one tenant with an initial tenant administrator principal.")
	fmt.Fprintln(out, "  bootstrap-platform-admin Grant the first platform_admin role to one OIDC principal.")
	fmt.Fprintln(out, "  disable-principal       Disable an existing principal by UUID.")
	fmt.Fprintln(out, "  show-tenant             Print one tenant row, including policy revision and members.")
	fmt.Fprintln(out, "  suspend-tenant          Mark one tenant SUSPENDED so its permissions are denied.")
	fmt.Fprintln(out, "  reactivate-tenant       Mark one tenant ACTIVE again.")
	fmt.Fprintln(out, "  revoke-session          Delete all console sessions for one principal.")
	fmt.Fprintln(out, "  show-revision           Read the current authorization revision for one tenant.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Common flags:")
	fmt.Fprintln(out, "  -database-url URL       PostgreSQL DSN. Defaults to ASTRASYNC_AUTH_DATABASE_URL.")
	fmt.Fprintln(out, "  -confirm                Required for mutating operations.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Per-operation flags are listed by running the operation with -help.")
}

func (c *adminCommand) run(ctx context.Context, args []string) error {
	for _, arg := range args {
		if arg == "-help" || arg == "--help" || arg == "-h" {
			printUsage(c.stdout)
			return nil
		}
	}
	flags := flag.NewFlagSet(string(c.operation), flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	databaseURL := flags.String("database-url", "", "PostgreSQL DSN (default ASTRASYNC_AUTH_DATABASE_URL)")
	confirm := flags.Bool("confirm", false, "Required for mutating operations")
	tenantID := flags.String("tenant-id", "", "Tenant UUID")
	tenantNamespace := flags.String("namespace", "", "Tenant namespace slug (required for bootstrap-tenant)")
	tenantDisplayName := flags.String("display-name", "", "Tenant display name")
	oidcIssuer := flags.String("oidc-issuer", "", "OIDC issuer URL (required for bootstrap flows)")
	oidcSubject := flags.String("oidc-subject", "", "OIDC subject claim (required for bootstrap flows)")
	principalID := flags.String("principal-id", "", "Principal UUID (required for disable/revoke)")
	switch c.operation {
	case opBootstrapTenant, opBootstrapPlatformAdmin, opDisablePrincipal, opShowTenant,
		opSuspendTenant, opReactivateTenant, opRevokeSession, opShowRevision:
	default:
		return fmt.Errorf("unknown operation %q", c.operation)
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := c.validateFlags(*confirm, *tenantID, *tenantNamespace, *tenantDisplayName, *oidcIssuer, *oidcSubject,
		*principalID); err != nil {
		return err
	}
	resolvedURL := strings.TrimSpace(*databaseURL)
	if resolvedURL == "" {
		resolvedURL = strings.TrimSpace(c.env("ASTRASYNC_AUTH_DATABASE_URL"))
	}
	if resolvedURL == "" {
		return errors.New("database URL is required (set -database-url or ASTRASYNC_AUTH_DATABASE_URL)")
	}
	repository, err := authpostgres.Open(ctx, resolvedURL)
	if err != nil {
		return err
	}
	defer repository.Close()
	if err := repository.Migrate(ctx); err != nil {
		return fmt.Errorf("apply authentication schema: %w", err)
	}
	switch c.operation {
	case opBootstrapTenant:
		return c.bootstrapTenant(ctx, repository, *tenantID, *tenantNamespace, *tenantDisplayName,
			*oidcIssuer, *oidcSubject)
	case opBootstrapPlatformAdmin:
		return c.bootstrapPlatformAdmin(ctx, repository, *oidcIssuer, *oidcSubject)
	case opDisablePrincipal:
		return c.disablePrincipal(ctx, repository, *principalID)
	case opShowTenant:
		return c.showTenant(ctx, repository, *tenantID)
	case opSuspendTenant:
		return c.setTenantStatus(ctx, repository, *tenantID, "SUSPENDED")
	case opReactivateTenant:
		return c.setTenantStatus(ctx, repository, *tenantID, "ACTIVE")
	case opRevokeSession:
		return c.revokeSessions(ctx, repository, *principalID)
	case opShowRevision:
		return c.showRevision(ctx, repository, *tenantID)
	default:
		return fmt.Errorf("unknown operation %q", c.operation)
	}
}

// validateFlags performs all parameter validation before the database is
// touched so operators see precise error messages for misconfiguration.
func (c *adminCommand) validateFlags(
	confirm bool, tenantID, namespace, displayName, issuer, subject, principalID string,
) error {
	mutating := c.operation == opBootstrapTenant ||
		c.operation == opBootstrapPlatformAdmin ||
		c.operation == opDisablePrincipal ||
		c.operation == opSuspendTenant ||
		c.operation == opReactivateTenant ||
		c.operation == opRevokeSession
	if mutating && !confirm {
		return fmt.Errorf("%s requires -confirm", c.operation)
	}
	if c.operation == opBootstrapTenant {
		if _, err := uuid.Parse(strings.TrimSpace(tenantID)); err != nil {
			return fmt.Errorf("tenant-id must be a canonical UUID: %w", err)
		}
		if strings.TrimSpace(namespace) == "" {
			return errors.New("namespace is required")
		}
		if err := auth.ValidateTenant(namespace, displayName); err != nil {
			return err
		}
		if strings.TrimSpace(issuer) == "" || strings.TrimSpace(subject) == "" {
			return errors.New("oidc-issuer and oidc-subject are required")
		}
		if err := auth.ValidateExternalIdentity(auth.ExternalIdentity{Issuer: issuer, Subject: subject}); err != nil {
			return err
		}
	}
	if c.operation == opBootstrapPlatformAdmin {
		if strings.TrimSpace(issuer) == "" || strings.TrimSpace(subject) == "" {
			return errors.New("oidc-issuer and oidc-subject are required")
		}
		if err := auth.ValidateExternalIdentity(auth.ExternalIdentity{Issuer: issuer, Subject: subject}); err != nil {
			return err
		}
	}
	if c.operation == opDisablePrincipal || c.operation == opRevokeSession {
		if _, err := uuid.Parse(strings.TrimSpace(principalID)); err != nil {
			return fmt.Errorf("principal-id must be a canonical UUID: %w", err)
		}
	}
	if c.operation == opShowTenant || c.operation == opSuspendTenant ||
		c.operation == opReactivateTenant || c.operation == opShowRevision {
		if _, err := uuid.Parse(strings.TrimSpace(tenantID)); err != nil {
			return fmt.Errorf("tenant-id must be a canonical UUID: %w", err)
		}
	}
	return nil
}

func (c *adminCommand) bootstrapTenant(
	ctx context.Context, repository *authpostgres.Repository,
	tenantID, namespace, displayName, issuer, subject string,
) error {
	if err := repository.BootstrapTenant(ctx, tenantID, namespace, displayName,
		auth.ExternalIdentity{Issuer: issuer, Subject: subject}); err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "tenant %s (%s) bootstrapped with administrator %s/%s\n",
		tenantID, namespace, issuer, subject)
	return nil
}

func (c *adminCommand) bootstrapPlatformAdmin(
	ctx context.Context, repository *authpostgres.Repository, issuer, subject string,
) error {
	if err := repository.BootstrapPlatformAdmin(ctx,
		auth.ExternalIdentity{Issuer: issuer, Subject: subject}); err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "platform_admin granted to %s/%s\n", issuer, subject)
	return nil
}

func (c *adminCommand) disablePrincipal(
	ctx context.Context, repository *authpostgres.Repository, principalID string,
) error {
	if err := repository.SetPrincipalStatus(ctx, principalID, "DISABLED"); err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "principal %s disabled\n", principalID)
	return nil
}

func (c *adminCommand) showTenant(
	ctx context.Context, repository *authpostgres.Repository, tenantID string,
) error {
	view, err := repository.ReadTenant(ctx, tenantID)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "tenant %s\n  namespace:       %s\n  display_name:    %s\n  status:          %s\n  authz_revision:  %d\n  members:         %d\n",
		view.TenantID, view.Namespace, view.DisplayName, view.Status, view.AuthzRevision, len(view.Members))
	for _, member := range view.Members {
		fmt.Fprintf(c.stdout, "    - %s role=%s status=%s\n", member.PrincipalID, member.Role, member.Status)
	}
	return nil
}

func (c *adminCommand) setTenantStatus(
	ctx context.Context, repository *authpostgres.Repository, tenantID, status string,
) error {
	if err := repository.SetTenantStatus(ctx, tenantID, status); err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "tenant %s status set to %s\n", tenantID, status)
	return nil
}

func (c *adminCommand) revokeSessions(
	ctx context.Context, repository *authpostgres.Repository, principalID string,
) error {
	count, err := repository.RevokeSessionsForPrincipal(ctx, principalID)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "revoked %d console sessions for principal %s\n", count, principalID)
	return nil
}

func (c *adminCommand) showRevision(
	ctx context.Context, repository *authpostgres.Repository, tenantID string,
) error {
	revision, err := repository.CurrentPolicyRevision(ctx, tenantID)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "tenant %s authz_revision=%s\n", tenantID, revision)
	return nil
}
