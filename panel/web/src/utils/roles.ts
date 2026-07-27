const roleLevel: Record<string, number> = {
  viewer: 1,
  operator: 2,
  admin: 3,
}

export function hasRequiredRole(role: string | undefined | null, minRole: string | undefined | null) {
  if (!minRole) return true
  if (!role) return false
  return (roleLevel[role] || 0) >= (roleLevel[minRole] || 0)
}

export function canOperate(role: string | undefined | null) {
  return hasRequiredRole(role, 'operator')
}

export function canAdminister(role: string | undefined | null) {
  return hasRequiredRole(role, 'admin')
}
