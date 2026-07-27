package organization

import (
	"errors"
	"strings"
	"time"
)

// Permission is an internal authorization capability (ADR 0019 does not publish it,
// and ADR 0017 keeps the catalog unpublished until custom roles ship). Authorization
// resolves a permission; no handler compares against a role name.
type Permission string

const (
	PermissionOrganizationRead  Permission = "organization.read"
	PermissionOrganizationWrite Permission = "organization.write"
	PermissionMonitorRead       Permission = "monitor.read"
	PermissionMonitorWrite      Permission = "monitor.write"
	PermissionMemberRead        Permission = "member.read"
	PermissionMemberWrite       Permission = "member.write"
)

// readSuffix marks a permission as read-only. Built-in roles are defined by rule
// rather than by a fixed list, so a permission added later is covered automatically
// (ADR 0017). Custom roles, when they ship, must not inherit that behavior.
const readSuffix = ".read"

// Role is an Organization role. Built-in names are reserved and a future custom role
// may not take them.
type Role string

const (
	RoleAdministrator Role = "Administrator"
	RoleViewer        Role = "Viewer"
)

// BuiltInRoles lists the roles this build defines, in descending capability order.
var BuiltInRoles = []Role{RoleAdministrator, RoleViewer}

// ValidRole reports whether a role is one this build defines.
func ValidRole(role Role) bool {
	for _, candidate := range BuiltInRoles {
		if candidate == role {
			return true
		}
	}
	return false
}

// Permits reports whether a role carries a permission. Administrator carries every
// permission including ones added later; Viewer carries every read permission.
func (role Role) Permits(permission Permission) bool {
	switch role {
	case RoleAdministrator:
		return true
	case RoleViewer:
		return strings.HasSuffix(string(permission), readSuffix)
	default:
		return false
	}
}

// Membership joins one instance user to one Organization with exactly one role.
type Membership struct {
	OrganizationID ID
	UserID         string
	Role           Role
	CreatedAt      time.Time
}

// NewMembership creates or restores a membership while enforcing its invariants.
func NewMembership(organizationID ID, userID string, role Role, createdAt time.Time) (Membership, error) {
	if organizationID == "" || userID == "" {
		return Membership{}, errors.New("a membership requires an Organization and a user")
	}
	if !ValidRole(role) {
		return Membership{}, errors.New("unknown Organization role")
	}
	if !isUTC(createdAt) {
		return Membership{}, errors.New("persisted timestamps must be UTC")
	}
	return Membership{
		OrganizationID: organizationID, UserID: userID, Role: role, CreatedAt: createdAt,
	}, nil
}
