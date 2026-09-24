package middleware

const (
	RoleViewer = "viewer"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

// Role is the wire enum for a user's role, surfaced on the api/system and
// api/auth response DTOs so trpcgo emits a "viewer" | "admin" | "owner" union
// instead of a bare string. Defined once here, in the package that owns the
// role constants, and referenced from both DTOs so the generated output keeps a
// single Role type. The values alias the Role* constants above, which remain
// the source HasMinRole ranks.
type Role string

const (
	RoleViewerWire Role = RoleViewer
	RoleAdminWire  Role = RoleAdmin
	RoleOwnerWire  Role = RoleOwner
)

var roleLevel = map[string]int{
	RoleViewer: 1,
	RoleAdmin:  2,
	RoleOwner:  3,
}

// HasMinRole reports whether role meets minRole in viewer < admin < owner
// order. Unknown roles rank below viewer.
func HasMinRole(role, minRole string) bool {
	return roleLevel[role] >= roleLevel[minRole]
}
