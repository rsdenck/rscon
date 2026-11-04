export interface User {
  id: string
  username: string
  email?: string
  display_name: string
  status: string
  mfa_enrolled: boolean
  last_login_at?: string
  created_at: string
}

export interface Environment {
  id: string
  name: string
  description: string
  critical: boolean
}

export interface Server {
  id: string
  name: string
  hostname: string
  address: string
  port: number
  environment_id: string
  environment?: string
  os_family: string
  location: string
  tags: string[]
  default_account: string
  host_key_fingerprint?: string
  enabled: boolean
  created_at: string
}

export interface Policy {
  id: string
  name: string
  description: string
  priority: number
  effect: 'allow' | 'deny'
  subjects: string[]
  actions: string[]
  server_ids: string[]
  server_group_ids: string[]
  environment_ids: string[]
  accounts: string[]
  allowed_channels: string[]
  require_approval: boolean
  approvals_required: number
  require_mfa: boolean
  max_duration_seconds: number
  time_windows: unknown[]
  enabled: boolean
}

export interface AccessRequest {
  id: string
  requester_id: string
  requester_name?: string
  server_id: string
  server_name?: string
  server_group_id?: string
  environment_id?: string
  account: string
  reason: string
  ticket?: string
  requested_seconds: number
  status: string
  starts_at?: string
  ends_at?: string
  approvals_required: number
  approvals_count: number
  created_at: string
}

export interface Session {
  id: string
  user_id: string
  username?: string
  server_id: string
  server_name?: string
  account: string
  source_ip: string
  auth_method: string
  gateway: string
  status: string
  command?: string
  started_at: string
  ended_at?: string
  duration_seconds: number
  termination_reason?: string
  recording_id?: string
}

export interface AuditEvent {
  id: number
  event_id: string
  category: string
  action: string
  actor_id?: string
  actor_name: string
  result: string
  resource_type?: string
  resource_id?: string
  source_ip?: string
  message?: string
  details?: Record<string, unknown>
  prev_hash: string
  hash: string
  created_at: string
}

export interface Dashboard {
  users: number
  servers: number
  active_sessions: number
  pending_access_requests: number
  policies: number
}

export interface Token {
  id: string
  kind: string
  expires_at: string
  last_used_at?: string
  revoked_at?: string
  created_at: string
}

export interface Page<T> {
  items: T[]
  total: number
  limit: number
  offset: number
}

export interface Role {
  id: string
  name: string
  description: string
  system: boolean
}

export interface Group {
  id: string
  name: string
  description: string
}
