package auth

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// TenantStatus enumerates allowed tenant lifecycle states. The values are kept
// in sync with the CHECK constraint in the authentication schema.
const (
	TenantStatusActive    = "ACTIVE"
	TenantStatusSuspended = "SUSPENDED"
)

// PrincipalStatus enumerates allowed principal lifecycle states.
const (
	PrincipalStatusActive   = "ACTIVE"
	PrincipalStatusDisabled = "DISABLED"
)

// PlatformRole enumerates allowed platform-role identifiers. Only platform_admin
// is supported in the first implementation.
const PlatformRoleAdmin = "platform_admin"

var (
	tenantNamespacePattern   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	tenantDisplayNamePattern = regexp.MustCompile(`^[\p{L}\p{N} _\-\.\(\)&]{0,256}$`)
	principalIssuerPattern   = regexp.MustCompile(`^https://[^\s/$.?#].[^\s]*$`)
	principalSubjectPattern  = regexp.MustCompile(`^[A-Za-z0-9._\-+=/]{1,512}$`)
)

// TenantView is the read model returned by the authentication repository for
// offline administrative inspection. It intentionally excludes session
// envelopes and token material.
type TenantView struct {
	TenantID      string
	Namespace     string
	DisplayName   string
	Status        string
	AuthzRevision int64
	Members       []TenantMember
}

// TenantMember describes one active or historical membership row.
type TenantMember struct {
	PrincipalID string
	Role        Role
	Status      string
}

// PlatformRoleGrant describes the persisted state of a platform-level role
// grant. The runtime uses it to project `AccessService.GrantPlatformRole`
// responses and to filter audit events.
type PlatformRoleGrant struct {
	PrincipalID string
	Role        string
	Active      bool
	GrantedAt   time.Time
	GrantedBy   string
}

// ValidateTenant returns an error if the proposed tenant identity cannot be
// stored. It enforces the same shape that the PostgreSQL schema requires.
func ValidateTenant(namespace, displayName string) error {
	if !tenantNamespacePattern.MatchString(namespace) {
		return errorWithMessage("tenant namespace must match DNS-1123 label")
	}
	if len(displayName) > 0 && !tenantDisplayNamePattern.MatchString(displayName) {
		return errorWithMessage("tenant display name is invalid")
	}
	return nil
}

// ValidateExternalIdentity ensures that bootstrap and provisioning flows only
// accept identity tuples the runtime can later resolve.
func ValidateExternalIdentity(identity ExternalIdentity) error {
	if !principalIssuerPattern.MatchString(identity.Issuer) {
		return errorWithMessage("OIDC issuer must be an HTTPS URL")
	}
	if !principalSubjectPattern.MatchString(identity.Subject) {
		return errorWithMessage("OIDC subject is invalid")
	}
	if strings.TrimSpace(identity.Issuer) == "" || strings.TrimSpace(identity.Subject) == "" {
		return errorWithMessage("OIDC identity must not be blank")
	}
	return nil
}

// SortedTenantRoles returns the supported tenant-role identifiers in stable
// order for documentation and tooling.
func SortedTenantRoles() []Role {
	roles := []Role{RoleTenantViewer, RoleTenantOperator, RoleTenantAuditor, RoleTenantAdmin}
	sort.Slice(roles, func(left, right int) bool { return string(roles[left]) < string(roles[right]) })
	return roles
}

type errorWithMessage string

func (e errorWithMessage) Error() string { return string(e) }
