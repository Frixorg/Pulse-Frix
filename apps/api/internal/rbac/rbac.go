// Package rbac defines explicit permissions and the roles that bundle them.
// Authorization is enforced at the API layer (never trusting the frontend) and
// again at the store layer via org scoping. See docs/DATA_MODEL.md#rbac.
package rbac

import "github.com/frix-me/pulse/api/internal/model"

// Permission is an explicit capability string.
type Permission string

const (
	ServerRead        Permission = "server.read"
	ServerManage      Permission = "server.manage"
	AlertRead         Permission = "alert.read"
	AlertManage       Permission = "alert.manage"
	EventRead         Permission = "event.read"
	IntegrationManage Permission = "integration.manage"
	// SSHExec gates the browser SSH console — the only capability in Pulse that
	// can change a server. Viewers never hold it.
	SSHExec    Permission = "ssh.exec"
	UserManage Permission = "user.manage"
	OrgManage  Permission = "org.manage"
	AuditRead  Permission = "audit.read"
)

// rolePermissions maps each role to the permissions it grants.
var rolePermissions = map[model.Role]map[Permission]bool{
	model.RoleViewer: {
		ServerRead: true, AlertRead: true, EventRead: true,
	},
	model.RoleAdmin: {
		ServerRead: true, AlertRead: true, EventRead: true,
		ServerManage: true, AlertManage: true, IntegrationManage: true, AuditRead: true,
		SSHExec: true,
	},
	model.RoleOwner: {
		ServerRead: true, AlertRead: true, EventRead: true,
		ServerManage: true, AlertManage: true, IntegrationManage: true, AuditRead: true,
		UserManage: true, OrgManage: true, SSHExec: true,
	},
}

// Can reports whether a role has a permission.
func Can(role model.Role, p Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[p]
}

// Permissions returns the sorted-ish set a role holds (for the session payload).
func Permissions(role model.Role) []Permission {
	var out []Permission
	for p := range rolePermissions[role] {
		out = append(out, p)
	}
	return out
}
