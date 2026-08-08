# Phase 6 Slice 18 Authorization Matrix

## Permission Vocabulary

| Permission | Resource scope | Meaning |
|---|---|---|
| `jobs.read` | Tenant | List Jobs and read Job/status details |
| `jobs.create` | Tenant | Create a Job in stopped/created state |
| `jobs.update` | Tenant | Replace an inactive Job specification with expected version |
| `jobs.start` | Tenant | Request running desired state with version fencing |
| `jobs.stop` | Tenant | Request stopped desired state with version fencing |
| `jobs.delete` | Tenant | Delete an eligible Job with expected version |
| `members.read` | Tenant | List tenant members and role assignments |
| `members.manage` | Tenant | Grant, replace, or revoke tenant roles |
| `audit.read` | Tenant | Query bounded tenant audit events |
| `tenants.create` | Platform | Create a tenant and initial tenant administrator |
| `platform.roles.manage` | Platform | Assign or revoke platform roles |
| `diagnostics.read` | Platform | Access protected diagnostics and gRPC reflection |

## Built-in Tenant Roles

| Role | Permissions |
|---|---|
| `tenant_viewer` | `jobs.read` |
| `tenant_operator` | `jobs.read`, `jobs.create`, `jobs.update`, `jobs.start`, `jobs.stop` |
| `tenant_auditor` | `jobs.read`, `members.read`, `audit.read` |
| `tenant_admin` | All tenant-scoped permissions, including `jobs.delete` and `members.manage` |

Tenant roles are mutually exclusive per principal/tenant in the first implementation. Replacing a
role uses an expected authorization revision and is audited. `tenant_admin` cannot assign a
platform role. Removing the last active tenant administrator is rejected unless another active
administrator is assigned in the same transaction.

## Platform Role

`platform_admin` grants all platform permissions and all tenant permissions. It is an explicit
database assignment, not an OIDC claim or wildcard membership. Every use records the platform role
and policy revision in the audit event. A future read-only platform support role is outside the
initial scope.

## JobService Method Mapping

| Full gRPC method | Permission | Scope source | Audit policy |
|---|---|---|---|
| `JobService/CreateJob` | `jobs.create` | `CreateJobRequest.namespace` | Required mutation event |
| `JobService/GetJob` | `jobs.read` | `GetJobRequest.namespace` | Policy-controlled successful read; always denied event |
| `JobService/ListJobs` | `jobs.read` | `ListJobsRequest.namespace` | Policy-controlled successful read; always denied event |
| `JobService/UpdateJob` | `jobs.update` | `UpdateJobRequest.namespace` | Required mutation event |
| `JobService/DeleteJob` | `jobs.delete` | `DeleteJobRequest.namespace` | Required mutation event |
| `JobService/StartJob` | `jobs.start` | `StartJobRequest.namespace` | Required mutation event |
| `JobService/StopJob` | `jobs.stop` | `StopJobRequest.namespace` | Required mutation event |
| `JobService/GetJobStatus` | `jobs.read` | `GetJobStatusRequest.namespace` | Policy-controlled successful read; always denied event |

The generated fully qualified method constants are used in code; the abbreviated names above are
for readability. A completeness test compares the service descriptor with the policy registry and
fails when either side has an unmatched method.

## Identity and Access Service Mapping

| Operation | Permission |
|---|---|
| Get current principal | Any authenticated active principal |
| List own tenants | Any authenticated active principal |
| List roles | Any authenticated active principal |
| List tenant members | `members.read` |
| Grant or replace tenant role | `members.manage` |
| Revoke tenant role | `members.manage` |
| List tenant audit events | `audit.read` |
| Create tenant | `tenants.create` |
| Manage platform role | `platform.roles.manage` |

## Decision Rules

1. Deny unauthenticated, inactive, expired, malformed, or unmapped requests.
2. Platform administrator permission is evaluated before tenant membership, but still requires an
   active tenant for tenant-scoped operations.
3. Tenant suspension denies every tenant permission.
4. No role grants access to another tenant by name similarity, OIDC domain, email, or group claim.
5. Authorization does not bypass Job expected-version or epoch checks.
6. A denied caller cannot distinguish a missing tenant/Job from an existing inaccessible one.
