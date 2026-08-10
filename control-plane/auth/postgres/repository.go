package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"io.astrasync/control-plane/auth"
)

//go:embed migrations/001_auth.sql
var migration string

//go:embed migrations/002_console_login.sql
var consoleLoginMigration string

type Repository struct {
	db    *sql.DB
	clock func() time.Time
	uid   func() string
}

func Open(ctx context.Context, dataSourceName string) (*Repository, error) {
	if dataSourceName == "" {
		return nil, fmt.Errorf("database URL must not be blank")
	}
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open auth PostgreSQL: %w", err)
	}
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("connect auth PostgreSQL: %w", err)
	}
	return New(database, time.Now, uuid.NewString), nil
}

func New(database *sql.DB, clock func() time.Time, uid func() string) *Repository {
	return &Repository{db: database, clock: clock, uid: uid}
}

func (r *Repository) Migrate(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("migrate authentication schema: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, consoleLoginMigration); err != nil {
		return fmt.Errorf("migrate Console authentication schema: %w", err)
	}
	return nil
}

func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

func (r *Repository) ResolveOrCreatePrincipal(
	ctx context.Context, identity auth.ExternalIdentity,
) (auth.Principal, error) {
	if identity.Issuer == "" || identity.Subject == "" {
		return auth.Principal{}, fmt.Errorf("external identity is incomplete")
	}
	now := r.clock().UTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO astrasync_auth_principals
            (principal_id, issuer, subject, status, created_at, updated_at)
         VALUES ($1::uuid, $2, $3, 'ACTIVE', $4, $4)
         ON CONFLICT (issuer, subject) DO NOTHING`,
		r.uid(), identity.Issuer, identity.Subject, now)
	if err != nil {
		return auth.Principal{}, fmt.Errorf("materialize OIDC principal: %w", err)
	}
	var principal auth.Principal
	var status string
	if err := r.db.QueryRowContext(ctx,
		`SELECT principal_id::text, issuer, subject, status
           FROM astrasync_auth_principals
          WHERE issuer = $1 AND subject = $2`,
		identity.Issuer, identity.Subject,
	).Scan(&principal.ID, &principal.Issuer, &principal.Subject, &status); err != nil {
		return auth.Principal{}, fmt.Errorf("read OIDC principal: %w", err)
	}
	principal.Active = status == "ACTIVE"
	principal.PolicyRevision = "principal:" + principal.ID
	principal.Memberships = make(map[string]auth.Membership)
	if !principal.Active {
		return principal, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.tenant_id::text, t.namespace, t.display_name, t.authz_revision, t.status, m.status, m.role_id
           FROM astrasync_auth_memberships m
           JOIN astrasync_auth_tenants t ON t.tenant_id = m.tenant_id
          WHERE m.principal_id = $1::uuid
          ORDER BY t.tenant_id`, principal.ID)
	if err != nil {
		return auth.Principal{}, fmt.Errorf("read principal memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID, namespace, displayName, tenantStatus, membershipStatus, roleID string
		var revision int64
		if err := rows.Scan(&tenantID, &namespace, &displayName, &revision, &tenantStatus, &membershipStatus, &roleID); err != nil {
			return auth.Principal{}, fmt.Errorf("scan principal membership: %w", err)
		}
		permissions := auth.PermissionsForRole(auth.Role(roleID))
		membership, err := auth.NewMembership(tenantID, tenantStatus == "ACTIVE" && membershipStatus == "ACTIVE", permissions...)
		if err != nil {
			return auth.Principal{}, fmt.Errorf("decode principal membership: %w", err)
		}
		membership.TenantNamespace = namespace
		membership.TenantDisplayName = displayName
		membership.Role = auth.Role(roleID)
		membership.PolicyRevision = strconv.FormatInt(revision, 10)
		principal.Memberships[tenantID] = membership
	}
	if err := rows.Err(); err != nil {
		return auth.Principal{}, fmt.Errorf("iterate principal memberships: %w", err)
	}
	if err := r.loadPlatformRole(ctx, &principal); err != nil {
		return auth.Principal{}, err
	}
	return principal, nil
}

func (r *Repository) ResolvePrincipalByID(ctx context.Context, principalID string) (auth.Principal, error) {
	if _, err := uuid.Parse(principalID); err != nil {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	var identity auth.ExternalIdentity
	if err := r.db.QueryRowContext(ctx,
		`SELECT issuer, subject FROM astrasync_auth_principals WHERE principal_id = $1::uuid`,
		principalID,
	).Scan(&identity.Issuer, &identity.Subject); errors.Is(err, sql.ErrNoRows) {
		return auth.Principal{}, auth.ErrUnauthenticated
	} else if err != nil {
		return auth.Principal{}, fmt.Errorf("read session principal identity: %w", err)
	}
	return r.ResolveOrCreatePrincipal(ctx, identity)
}

func (r *Repository) loadPlatformRole(ctx context.Context, principal *auth.Principal) error {
	var active bool
	if err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
            SELECT 1 FROM astrasync_auth_platform_roles
             WHERE principal_id = $1::uuid AND role_id = 'platform_admin' AND status = 'ACTIVE'
         )`, principal.ID).Scan(&active); err != nil {
		return fmt.Errorf("read principal platform role: %w", err)
	}
	principal.PlatformAdmin = active
	if !active {
		return nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT tenant_id::text, namespace, display_name, authz_revision
           FROM astrasync_auth_tenants
          WHERE status = 'ACTIVE'
          ORDER BY tenant_id`)
	if err != nil {
		return fmt.Errorf("read platform administrator tenants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID, namespace, displayName string
		var revision int64
		if err := rows.Scan(&tenantID, &namespace, &displayName, &revision); err != nil {
			return fmt.Errorf("scan platform administrator tenant: %w", err)
		}
		if _, exists := principal.Memberships[tenantID]; exists {
			continue
		}
		membership, err := auth.NewMembership(tenantID, true, auth.PermissionsForRole(auth.RolePlatformAdmin)...)
		if err != nil {
			return err
		}
		membership.TenantNamespace = namespace
		membership.TenantDisplayName = displayName
		membership.Role = auth.RolePlatformAdmin
		membership.PolicyRevision = strconv.FormatInt(revision, 10)
		principal.Memberships[tenantID] = membership
	}
	return rows.Err()
}

func (r *Repository) CurrentPolicyRevision(ctx context.Context, tenantID string) (string, error) {
	var revision int64
	var status string
	if err := r.db.QueryRowContext(ctx,
		`SELECT authz_revision, status FROM astrasync_auth_tenants WHERE tenant_id = $1::uuid`, tenantID,
	).Scan(&revision, &status); errors.Is(err, sql.ErrNoRows) {
		return "", auth.ErrTenantUnavailable
	} else if err != nil {
		return "", fmt.Errorf("read tenant policy revision: %w", err)
	}
	if status != "ACTIVE" {
		return "", auth.ErrTenantUnavailable
	}
	return strconv.FormatInt(revision, 10), nil
}

func (r *Repository) BootstrapTenant(
	ctx context.Context,
	tenantID, namespace, displayName string,
	identity auth.ExternalIdentity,
) error {
	if _, err := uuid.Parse(tenantID); err != nil || namespace == "" || identity.Issuer == "" || identity.Subject == "" {
		return fmt.Errorf("bootstrap tenant identity is invalid")
	}
	now := r.clock().UTC()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin tenant bootstrap: %w", err)
	}
	defer tx.Rollback()
	principalID := r.uid()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_auth_principals
            (principal_id, issuer, subject, status, created_at, updated_at)
         VALUES ($1::uuid, $2, $3, 'ACTIVE', $4, $4)
         ON CONFLICT (issuer, subject) DO NOTHING`,
		principalID, identity.Issuer, identity.Subject, now); err != nil {
		return fmt.Errorf("bootstrap principal: %w", err)
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT principal_id::text FROM astrasync_auth_principals WHERE issuer = $1 AND subject = $2`,
		identity.Issuer, identity.Subject).Scan(&principalID); err != nil {
		return fmt.Errorf("resolve bootstrap principal: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_auth_tenants
            (tenant_id, namespace, display_name, status, authz_revision, created_at, updated_at)
         VALUES ($1::uuid, $2, $3, 'ACTIVE', 1, $4, $4)
         ON CONFLICT (tenant_id) DO NOTHING`, tenantID, namespace, displayName, now); err != nil {
		return fmt.Errorf("bootstrap tenant: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_auth_memberships
            (tenant_id, principal_id, role_id, status, granted_by, granted_at, updated_at)
         VALUES ($1::uuid, $2::uuid, 'tenant_admin', 'ACTIVE', 'bootstrap', $3, $3)
         ON CONFLICT (tenant_id, principal_id) DO NOTHING`, tenantID, principalID, now); err != nil {
		return fmt.Errorf("bootstrap tenant administrator: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tenant bootstrap: %w", err)
	}
	return nil
}

func (r *Repository) WriteSecurityAudit(ctx context.Context, event auth.SecurityAuditEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	attributes, err := json.Marshal(event.Attributes)
	if err != nil {
		return fmt.Errorf("encode security audit attributes: %w", err)
	}
	var tenantID any
	if event.TenantID != "" {
		tenantID = event.TenantID
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO astrasync_security_audit_events
            (event_id, event_type, actor_id, tenant_id, request_id, outcome, attributes, occurred_at)
         VALUES ($1, $2, $3, $4::uuid, $5, $6, $7::jsonb, $8)`,
		event.EventID, event.EventType, event.ActorID, tenantID, event.RequestID,
		event.Outcome, attributes, event.OccurredAt)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return fmt.Errorf("security audit event already exists")
		}
		return fmt.Errorf("write security audit event: %w", err)
	}
	return nil
}

var _ auth.IdentityResolver = (*Repository)(nil)
var _ auth.AuditWriter = (*Repository)(nil)
