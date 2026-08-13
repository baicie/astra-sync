package postgres

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

//go:embed migrations/003_audit_query.sql
var auditQueryMigration string

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
	if _, err := r.db.ExecContext(ctx, auditQueryMigration); err != nil {
		return fmt.Errorf("migrate audit query schema: %w", err)
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

func (r *Repository) ListSecurityAudit(
	ctx context.Context, query auth.SecurityAuditQuery,
) ([]auth.SecurityAuditEvent, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	var cursorTime any
	var cursorEventID any
	if query.Cursor != nil {
		cursorTime = query.Cursor.OccurredAt.UTC()
		cursorEventID = query.Cursor.EventID
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT event_id, event_type, actor_id, tenant_id::text, request_id,
                outcome, attributes, occurred_at
           FROM astrasync_security_audit_events
          WHERE tenant_id = $1::uuid
            AND occurred_at >= $2
            AND occurred_at <= $3
			AND ($4::text = '' OR event_type = ANY(string_to_array($4::text, ',')))
			AND ($5::text = '' OR outcome = ANY(string_to_array($5::text, ',')))
            AND ($6::timestamptz IS NULL OR occurred_at < $6 OR
                 (occurred_at = $6 AND event_id < $7::text))
          ORDER BY occurred_at DESC, event_id DESC
          LIMIT $8`,
		query.TenantID, query.OccurredAfter.UTC(), query.OccurredBefore.UTC(),
		strings.Join(query.EventTypes, ","), strings.Join(query.Outcomes, ","),
		cursorTime, cursorEventID, query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query security audit events: %w", err)
	}
	defer rows.Close()
	result := make([]auth.SecurityAuditEvent, 0, query.Limit)
	for rows.Next() {
		var event auth.SecurityAuditEvent
		var attributes []byte
		if err := rows.Scan(
			&event.EventID, &event.EventType, &event.ActorID, &event.TenantID,
			&event.RequestID, &event.Outcome, &attributes, &event.OccurredAt,
		); err != nil {
			return nil, fmt.Errorf("scan security audit event: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(attributes))
		decoder.UseNumber()
		if err := decoder.Decode(&event.Attributes); err != nil {
			return nil, fmt.Errorf("decode security audit attributes: %w", err)
		}
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("stored security audit event is invalid: %w", err)
		}
		event.OccurredAt = event.OccurredAt.UTC()
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate security audit events: %w", err)
	}
	return result, nil
}

var _ auth.IdentityResolver = (*Repository)(nil)
var _ auth.AuditWriter = (*Repository)(nil)
var _ auth.AuditReader = (*Repository)(nil)
var _ auth.AccessRepository = (*Repository)(nil)

// BootstrapPlatformAdmin grants the platform_admin role to one OIDC principal
// identified by its issuer and subject. The operation is idempotent: a
// pre-existing ACTIVE platform assignment is left untouched, while a disabled
// assignment is reactivated. The principal must already exist; provisioning new
// principals outside the request path is the responsibility of
// ResolveOrCreatePrincipal.
func (r *Repository) BootstrapPlatformAdmin(ctx context.Context, identity auth.ExternalIdentity) error {
	if err := auth.ValidateExternalIdentity(identity); err != nil {
		return err
	}
	now := r.clock().UTC()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin platform-admin bootstrap: %w", err)
	}
	defer tx.Rollback()
	var principalID string
	if err := tx.QueryRowContext(ctx,
		`SELECT principal_id::text FROM astrasync_auth_principals WHERE issuer = $1 AND subject = $2`,
		identity.Issuer, identity.Subject,
	).Scan(&principalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("principal %s/%s has not authenticated yet", identity.Issuer, identity.Subject)
		}
		return fmt.Errorf("read bootstrap principal: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_auth_platform_roles
            (principal_id, role_id, status, granted_by, granted_at)
         VALUES ($1::uuid, $2, 'ACTIVE', 'bootstrap', $3)
         ON CONFLICT (principal_id, role_id) DO UPDATE
            SET status = 'ACTIVE', granted_by = 'bootstrap', granted_at = EXCLUDED.granted_at`,
		principalID, auth.PlatformRoleAdmin, now,
	); err != nil {
		return fmt.Errorf("grant platform administrator: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit platform-admin bootstrap: %w", err)
	}
	return nil
}

// SetPrincipalStatus flips an existing principal between ACTIVE and DISABLED.
// It returns ErrUnauthenticated when the principal UUID is unknown.
func (r *Repository) SetPrincipalStatus(ctx context.Context, principalID, status string) error {
	if _, err := uuid.Parse(principalID); err != nil {
		return auth.ErrUnauthenticated
	}
	if status != auth.PrincipalStatusActive && status != auth.PrincipalStatusDisabled {
		return fmt.Errorf("principal status must be ACTIVE or DISABLED")
	}
	now := r.clock().UTC()
	result, err := r.db.ExecContext(ctx,
		`UPDATE astrasync_auth_principals SET status = $1, updated_at = $2 WHERE principal_id = $3::uuid`,
		status, now, principalID,
	)
	if err != nil {
		return fmt.Errorf("update principal status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("principal status rows affected: %w", err)
	}
	if rows == 0 {
		return auth.ErrUnauthenticated
	}
	return nil
}

// SetTenantStatus transitions a tenant between ACTIVE and SUSPENDED. A
// SUSPENDED tenant denies every tenant permission at the authorization
// interceptor.
func (r *Repository) SetTenantStatus(ctx context.Context, tenantID, status string) error {
	if _, err := uuid.Parse(tenantID); err != nil {
		return auth.ErrTenantUnavailable
	}
	if status != auth.TenantStatusActive && status != auth.TenantStatusSuspended {
		return fmt.Errorf("tenant status must be ACTIVE or SUSPENDED")
	}
	now := r.clock().UTC()
	result, err := r.db.ExecContext(ctx,
		`UPDATE astrasync_auth_tenants SET status = $1, updated_at = $2 WHERE tenant_id = $3::uuid`,
		status, now, tenantID,
	)
	if err != nil {
		return fmt.Errorf("update tenant status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("tenant status rows affected: %w", err)
	}
	if rows == 0 {
		return auth.ErrTenantUnavailable
	}
	return nil
}

// ReadTenant returns the public read model for one tenant, including active and
// disabled membership rows. It does not include session material.
func (r *Repository) ReadTenant(ctx context.Context, tenantID string) (auth.TenantView, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return auth.TenantView{}, auth.ErrTenantUnavailable
	}
	view := auth.TenantView{Members: []auth.TenantMember{}}
	if err := r.db.QueryRowContext(ctx,
		`SELECT tenant_id::text, namespace, display_name, status, authz_revision
           FROM astrasync_auth_tenants WHERE tenant_id = $1::uuid`, tenantID,
	).Scan(&view.TenantID, &view.Namespace, &view.DisplayName, &view.Status, &view.AuthzRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auth.TenantView{}, auth.ErrTenantUnavailable
		}
		return auth.TenantView{}, fmt.Errorf("read tenant: %w", err)
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT principal_id::text, role_id, status FROM astrasync_auth_memberships
          WHERE tenant_id = $1::uuid ORDER BY principal_id`, tenantID,
	)
	if err != nil {
		return auth.TenantView{}, fmt.Errorf("read tenant members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var member auth.TenantMember
		if err := rows.Scan(&member.PrincipalID, &member.Role, &member.Status); err != nil {
			return auth.TenantView{}, fmt.Errorf("scan tenant member: %w", err)
		}
		view.Members = append(view.Members, member)
	}
	if err := rows.Err(); err != nil {
		return auth.TenantView{}, fmt.Errorf("iterate tenant members: %w", err)
	}
	return view, nil
}

// RevokeSessionsForPrincipal deletes every console session for the given
// principal. It returns the number of sessions removed.
func (r *Repository) RevokeSessionsForPrincipal(ctx context.Context, principalID string) (int64, error) {
	if _, err := uuid.Parse(principalID); err != nil {
		return 0, auth.ErrUnauthenticated
	}
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM astrasync_auth_sessions WHERE principal_id = $1::uuid`, principalID,
	)
	if err != nil {
		return 0, fmt.Errorf("revoke principal sessions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("session rows affected: %w", err)
	}
	return count, nil
}

// LoadTenantMembers returns the roster of ACTIVE and DISABLED memberships for a
// tenant. The membership view is intended for `AccessService.ListMembers` and
// the administrative console.
func (r *Repository) LoadTenantMembers(ctx context.Context, tenantID string) ([]auth.TenantMember, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, auth.ErrTenantUnavailable
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT principal_id::text, role_id, status
           FROM astrasync_auth_memberships
          WHERE tenant_id = $1::uuid
          ORDER BY principal_id`, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("load tenant members: %w", err)
	}
	defer rows.Close()
	members := make([]auth.TenantMember, 0, 8)
	for rows.Next() {
		var member auth.TenantMember
		var roleID string
		if err := rows.Scan(&member.PrincipalID, &roleID, &member.Status); err != nil {
			return nil, fmt.Errorf("scan tenant member: %w", err)
		}
		member.Role = auth.Role(roleID)
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant members: %w", err)
	}
	return members, nil
}

// GrantTenantRole inserts (or reactivates) a tenant membership row for the
// supplied principal/role pair and writes the matching security audit event
// in the same PostgreSQL transaction. The tenant's authz_revision is also
// incremented so a stale policy cannot survive a successful grant.
func (r *Repository) GrantTenantRole(
	ctx context.Context, tenantID, principalID string, role auth.Role,
	actorID string, audit auth.SecurityAuditEvent,
) (auth.TenantView, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return auth.TenantView{}, auth.ErrTenantUnavailable
	}
	if _, err := uuid.Parse(principalID); err != nil {
		return auth.TenantView{}, auth.ErrUnauthenticated
	}
	if !validTenantRole(role) {
		return auth.TenantView{}, fmt.Errorf("tenant role %q is not supported", role)
	}
	if err := audit.Validate(); err != nil {
		return auth.TenantView{}, err
	}
	now := r.clock().UTC()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return auth.TenantView{}, fmt.Errorf("begin tenant role grant: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_auth_memberships
            (tenant_id, principal_id, role_id, status, granted_by, granted_at, updated_at)
         VALUES ($1::uuid, $2::uuid, $3, 'ACTIVE', $4, $5, $5)
         ON CONFLICT (tenant_id, principal_id) DO UPDATE
            SET role_id = EXCLUDED.role_id,
                status = 'ACTIVE',
                granted_by = EXCLUDED.granted_by,
                granted_at = EXCLUDED.granted_at,
                updated_at = EXCLUDED.updated_at`,
		tenantID, principalID, string(role), actorID, now,
	); err != nil {
		return auth.TenantView{}, fmt.Errorf("upsert tenant membership: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE astrasync_auth_tenants
            SET authz_revision = authz_revision + 1,
                updated_at = $1
          WHERE tenant_id = $2::uuid`,
		now, tenantID,
	); err != nil {
		return auth.TenantView{}, fmt.Errorf("bump tenant authz revision: %w", err)
	}
	if err := writeAuditInTx(ctx, tx, audit); err != nil {
		return auth.TenantView{}, err
	}
	view, err := readTenantInTx(ctx, tx, tenantID)
	if err != nil {
		return auth.TenantView{}, err
	}
	if err := tx.Commit(); err != nil {
		return auth.TenantView{}, fmt.Errorf("commit tenant role grant: %w", err)
	}
	return view, nil
}

// RevokeTenantRole disables an existing membership row, bumps the tenant's
// authz_revision, and writes the matching security audit event inside a single
// PostgreSQL transaction.
func (r *Repository) RevokeTenantRole(
	ctx context.Context, tenantID, principalID, actorID string, audit auth.SecurityAuditEvent,
) (auth.TenantView, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return auth.TenantView{}, auth.ErrTenantUnavailable
	}
	if _, err := uuid.Parse(principalID); err != nil {
		return auth.TenantView{}, auth.ErrUnauthenticated
	}
	if err := audit.Validate(); err != nil {
		return auth.TenantView{}, err
	}
	now := r.clock().UTC()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return auth.TenantView{}, fmt.Errorf("begin tenant role revoke: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_auth_memberships
            (tenant_id, principal_id, role_id, status, granted_by, granted_at, updated_at)
         VALUES ($1::uuid, $2::uuid, 'tenant_viewer', 'DISABLED', $3, $4, $4)
         ON CONFLICT (tenant_id, principal_id) DO UPDATE
            SET status = 'DISABLED',
                granted_by = EXCLUDED.granted_by,
                granted_at = EXCLUDED.granted_at,
                updated_at = EXCLUDED.updated_at`,
		tenantID, principalID, actorID, now,
	); err != nil {
		return auth.TenantView{}, fmt.Errorf("disable tenant membership: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE astrasync_auth_tenants
            SET authz_revision = authz_revision + 1,
                updated_at = $1
          WHERE tenant_id = $2::uuid`,
		now, tenantID,
	); err != nil {
		return auth.TenantView{}, fmt.Errorf("bump tenant authz revision: %w", err)
	}
	if err := writeAuditInTx(ctx, tx, audit); err != nil {
		return auth.TenantView{}, err
	}
	view, err := readTenantInTx(ctx, tx, tenantID)
	if err != nil {
		return auth.TenantView{}, err
	}
	if err := tx.Commit(); err != nil {
		return auth.TenantView{}, fmt.Errorf("commit tenant role revoke: %w", err)
	}
	return view, nil
}

// GrantPlatformRole inserts (or reactivates) a platform_admin grant and writes
// the matching security audit event in a single transaction. The platform role
// table is the single source of truth for cross-tenant privileges.
func (r *Repository) GrantPlatformRole(
	ctx context.Context, principalID string, role string,
	actorID string, audit auth.SecurityAuditEvent,
) (auth.PlatformRoleGrant, error) {
	if _, err := uuid.Parse(principalID); err != nil {
		return auth.PlatformRoleGrant{}, auth.ErrUnauthenticated
	}
	if role != auth.PlatformRoleAdmin {
		return auth.PlatformRoleGrant{}, fmt.Errorf("platform role %q is not supported", role)
	}
	if err := audit.Validate(); err != nil {
		return auth.PlatformRoleGrant{}, err
	}
	now := r.clock().UTC()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return auth.PlatformRoleGrant{}, fmt.Errorf("begin platform role grant: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_auth_platform_roles
            (principal_id, role_id, status, granted_by, granted_at)
         VALUES ($1::uuid, $2, 'ACTIVE', $3, $4)
         ON CONFLICT (principal_id, role_id) DO UPDATE
            SET status = 'ACTIVE',
                granted_by = EXCLUDED.granted_by,
                granted_at = EXCLUDED.granted_at`,
		principalID, role, actorID, now,
	); err != nil {
		return auth.PlatformRoleGrant{}, fmt.Errorf("grant platform role: %w", err)
	}
	if err := writeAuditInTx(ctx, tx, audit); err != nil {
		return auth.PlatformRoleGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return auth.PlatformRoleGrant{}, fmt.Errorf("commit platform role grant: %w", err)
	}
	return auth.PlatformRoleGrant{
		PrincipalID: principalID, Role: role, Active: true,
		GrantedAt: now, GrantedBy: actorID,
	}, nil
}

// RevokePlatformRole disables an existing platform_admin grant and writes the
// matching security audit event in a single transaction. A non-existent grant
// is treated as already-disabled (idempotent).
func (r *Repository) RevokePlatformRole(
	ctx context.Context, principalID string, role string,
	actorID string, audit auth.SecurityAuditEvent,
) (auth.PlatformRoleGrant, error) {
	if _, err := uuid.Parse(principalID); err != nil {
		return auth.PlatformRoleGrant{}, auth.ErrUnauthenticated
	}
	if role != auth.PlatformRoleAdmin {
		return auth.PlatformRoleGrant{}, fmt.Errorf("platform role %q is not supported", role)
	}
	if err := audit.Validate(); err != nil {
		return auth.PlatformRoleGrant{}, err
	}
	now := r.clock().UTC()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return auth.PlatformRoleGrant{}, fmt.Errorf("begin platform role revoke: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_auth_platform_roles
            (principal_id, role_id, status, granted_by, granted_at)
         VALUES ($1::uuid, $2, 'DISABLED', $3, $4)
         ON CONFLICT (principal_id, role_id) DO UPDATE
            SET status = 'DISABLED',
                granted_by = EXCLUDED.granted_by,
                granted_at = EXCLUDED.granted_at`,
		principalID, role, actorID, now,
	); err != nil {
		return auth.PlatformRoleGrant{}, fmt.Errorf("revoke platform role: %w", err)
	}
	if err := writeAuditInTx(ctx, tx, audit); err != nil {
		return auth.PlatformRoleGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return auth.PlatformRoleGrant{}, fmt.Errorf("commit platform role revoke: %w", err)
	}
	return auth.PlatformRoleGrant{
		PrincipalID: principalID, Role: role, Active: false,
		GrantedAt: now, GrantedBy: actorID,
	}, nil
}

func writeAuditInTx(ctx context.Context, tx *sql.Tx, event auth.SecurityAuditEvent) error {
	attributes, err := json.Marshal(event.Attributes)
	if err != nil {
		return fmt.Errorf("encode security audit attributes: %w", err)
	}
	var tenantID any
	if event.TenantID != "" {
		tenantID = event.TenantID
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_security_audit_events
            (event_id, event_type, actor_id, tenant_id, request_id, outcome, attributes, occurred_at)
         VALUES ($1, $2, $3, $4::uuid, $5, $6, $7::jsonb, $8)`,
		event.EventID, event.EventType, event.ActorID, tenantID, event.RequestID,
		event.Outcome, attributes, event.OccurredAt,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("security audit event already exists")
		}
		return fmt.Errorf("write security audit in transaction: %w", err)
	}
	return nil
}

func readTenantInTx(ctx context.Context, tx *sql.Tx, tenantID string) (auth.TenantView, error) {
	view := auth.TenantView{TenantID: tenantID, Members: []auth.TenantMember{}}
	if err := tx.QueryRowContext(ctx,
		`SELECT namespace, display_name, status, authz_revision
           FROM astrasync_auth_tenants WHERE tenant_id = $1::uuid`, tenantID,
	).Scan(&view.Namespace, &view.DisplayName, &view.Status, &view.AuthzRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auth.TenantView{}, auth.ErrTenantUnavailable
		}
		return auth.TenantView{}, fmt.Errorf("read tenant in transaction: %w", err)
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT principal_id::text, role_id, status
           FROM astrasync_auth_memberships
          WHERE tenant_id = $1::uuid
          ORDER BY principal_id`, tenantID,
	)
	if err != nil {
		return auth.TenantView{}, fmt.Errorf("read tenant members in transaction: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var member auth.TenantMember
		var roleID string
		if err := rows.Scan(&member.PrincipalID, &roleID, &member.Status); err != nil {
			return auth.TenantView{}, fmt.Errorf("scan tenant member in transaction: %w", err)
		}
		member.Role = auth.Role(roleID)
		view.Members = append(view.Members, member)
	}
	if err := rows.Err(); err != nil {
		return auth.TenantView{}, fmt.Errorf("iterate tenant members in transaction: %w", err)
	}
	return view, nil
}

func validTenantRole(role auth.Role) bool {
	switch role {
	case auth.RoleTenantViewer, auth.RoleTenantOperator, auth.RoleTenantAuditor, auth.RoleTenantAdmin:
		return true
	default:
		return false
	}
}
