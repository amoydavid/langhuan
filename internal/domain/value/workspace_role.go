package value

// WorkspaceRole 是 workspace 内的角色（owner | admin | member）。
// 顺序为 member < admin < owner，rank 越大权限越高。
type WorkspaceRole string

const (
	RoleMember WorkspaceRole = "member"
	RoleAdmin  WorkspaceRole = "admin"
	RoleOwner  WorkspaceRole = "owner"
)

// roleRank 定义各角色的权限等级，越大越高。
var roleRank = map[WorkspaceRole]int{
	RoleMember: 1,
	RoleAdmin:  2,
	RoleOwner:  3,
}

// Rank 返回角色的权限等级（member=1, admin=2, owner=3）。
// 未知角色返回 0，从而低于任何已知角色。
func (r WorkspaceRole) Rank() int {
	if rank, ok := roleRank[r]; ok {
		return rank
	}
	return 0
}

// AtLeast 判断当前角色是否达到 min 的等级（含等于）。
func (r WorkspaceRole) AtLeast(min WorkspaceRole) bool {
	return r.Rank() >= min.Rank()
}

// IsValid 判断角色是否为已知的 owner/admin/member 之一。
func (r WorkspaceRole) IsValid() bool {
	_, ok := roleRank[r]
	return ok
}
