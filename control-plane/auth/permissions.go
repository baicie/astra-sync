// Package auth defines authenticated principals and tenant-scoped authorization policy.
package auth

import "sort"

type Permission string

const (
	PermissionJobsRead           Permission = "jobs.read"
	PermissionJobsCreate         Permission = "jobs.create"
	PermissionJobsUpdate         Permission = "jobs.update"
	PermissionJobsStart          Permission = "jobs.start"
	PermissionJobsStop           Permission = "jobs.stop"
	PermissionJobsDelete         Permission = "jobs.delete"
	PermissionMembersRead        Permission = "members.read"
	PermissionMembersManage      Permission = "members.manage"
	PermissionAuditRead          Permission = "audit.read"
	PermissionTenantsCreate      Permission = "tenants.create"
	PermissionPlatformRoles      Permission = "platform.roles.manage"
	PermissionDiagnosticsRead    Permission = "diagnostics.read"
	PermissionConnectorsRead     Permission = "connectors.read"
	PermissionConnectionsRead    Permission = "connections.read"
	PermissionConnectionsCreate  Permission = "connections.create"
	PermissionConnectionsUpdate  Permission = "connections.update"
	PermissionConnectionsUse     Permission = "connections.use"
	PermissionConnectionsTest    Permission = "connections.test"
	PermissionConnectionsRotate  Permission = "connections.rotate"
	PermissionConnectionsDisable Permission = "connections.disable"
	PermissionConnectionsDelete  Permission = "connections.delete"
)

type Role string

const (
	RoleTenantViewer   Role = "tenant_viewer"
	RoleTenantOperator Role = "tenant_operator"
	RoleTenantAuditor  Role = "tenant_auditor"
	RoleTenantAdmin    Role = "tenant_admin"
	RolePlatformAdmin  Role = "platform_admin"
)

var allConnectionPermissions = []Permission{
	PermissionConnectorsRead,
	PermissionConnectionsRead,
	PermissionConnectionsCreate,
	PermissionConnectionsUpdate,
	PermissionConnectionsUse,
	PermissionConnectionsTest,
	PermissionConnectionsRotate,
	PermissionConnectionsDisable,
	PermissionConnectionsDelete,
}

var allTenantPermissions = []Permission{
	PermissionJobsRead,
	PermissionJobsCreate,
	PermissionJobsUpdate,
	PermissionJobsStart,
	PermissionJobsStop,
	PermissionJobsDelete,
	PermissionMembersRead,
	PermissionMembersManage,
	PermissionAuditRead,
	PermissionConnectorsRead,
	PermissionConnectionsRead,
	PermissionConnectionsCreate,
	PermissionConnectionsUpdate,
	PermissionConnectionsUse,
	PermissionConnectionsTest,
	PermissionConnectionsRotate,
	PermissionConnectionsDisable,
	PermissionConnectionsDelete,
}

func PermissionsForRole(role Role) []Permission {
	var permissions []Permission
	switch role {
	case RoleTenantViewer:
		permissions = []Permission{PermissionJobsRead, PermissionConnectorsRead}
	case RoleTenantOperator:
		permissions = []Permission{
			PermissionJobsRead,
			PermissionJobsCreate,
			PermissionJobsUpdate,
			PermissionJobsStart,
			PermissionJobsStop,
			PermissionConnectorsRead,
			PermissionConnectionsRead,
			PermissionConnectionsUse,
			PermissionConnectionsTest,
		}
	case RoleTenantAuditor:
		permissions = []Permission{
			PermissionJobsRead,
			PermissionMembersRead,
			PermissionAuditRead,
			PermissionConnectorsRead,
		}
	case RoleTenantAdmin:
		permissions = allTenantPermissions
	case RolePlatformAdmin:
		permissions = append(append([]Permission(nil), allTenantPermissions...),
			PermissionTenantsCreate, PermissionPlatformRoles, PermissionDiagnosticsRead)
	default:
		return nil
	}
	result := append([]Permission(nil), permissions...)
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func AllTenantPermissions() []Permission {
	result := append([]Permission(nil), allTenantPermissions...)
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func AllConnectionPermissions() []Permission {
	result := append([]Permission(nil), allConnectionPermissions...)
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}
