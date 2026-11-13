package access

// Role is what a user may do, on the whole instance or on one project. The
// API token abilities (read, deploy, write) stay the credential contract; a
// role maps to the abilities it grants, and a request needs both.
type Role string

const (
	Owner     Role = "owner"
	Admin     Role = "admin"
	Developer Role = "developer"
	Viewer    Role = "viewer"
)

// rank orders the roles; an unknown or empty role ranks 0 (no access).
var rank = map[Role]int{Viewer: 1, Developer: 2, Admin: 3, Owner: 4}

func (r Role) Valid() bool { return rank[r] > 0 }

// AtLeast reports whether r grants everything other grants.
func (r Role) AtLeast(other Role) bool { return r.Valid() && rank[r] >= rank[other] }

// Allows maps the role to the token abilities: every role reads, developer
// deploys, admin and owner write.
func (r Role) Allows(ability string) bool {
	switch ability {
	case "read":
		return r.AtLeast(Viewer)
	case "deploy":
		return r.AtLeast(Developer)
	case "write":
		return r.AtLeast(Admin)
	}
	return false
}

// higher returns the stronger of two roles; either may be empty.
func higher(a, b Role) Role {
	if rank[b] > rank[a] {
		return b
	}
	return a
}
