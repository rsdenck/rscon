// Package model defines the RSCON domain types shared by the store, policy
// engine, API and SSH data plane.
package model

import "time"

// Account and record statuses.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusLocked   = "locked"

	RequestPending  = "pending"
	RequestApproved = "approved"
	RequestDenied   = "denied"
	RequestExpired  = "expired"
	RequestCanceled = "canceled"

	DecisionApprove = "approve"
	DecisionDeny    = "deny"

	SessionActive      = "active"
	SessionClosed      = "closed"
	SessionFailed      = "failed"
	SessionInterrupted = "interrupted"

	EffectAllow = "allow"
	EffectDeny  = "deny"

	TokenSession = "session"
	TokenAPI     = "api"
)

// User is a RSCON identity.
type User struct {
	ID               string     `json:"id"`
	Username         string     `json:"username"`
	Email            string     `json:"email,omitempty"`
	DisplayName      string     `json:"display_name"`
	PasswordHash     string     `json:"-"`
	Status           string     `json:"status"`
	MFAEnrolled      bool       `json:"mfa_enrolled"`
	IdentityProvider string     `json:"identity_provider,omitempty"`
	LastLoginAt      *time.Time `json:"last_login_at,omitempty"`
	FailedLogins     int        `json:"failed_logins"`
	LockedUntil      *time.Time `json:"locked_until,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

// Active reports whether the account may authenticate.
func (u *User) Active() bool { return u.Status == StatusActive && u.DeletedAt == nil }

// Group is a collection of users used for policy subjects.
type Group struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// Role grants permissions to users or groups.
type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	System      bool      `json:"system"`
	CreatedAt   time.Time `json:"created_at"`
}

// Permission is a capability such as ssh.connect or audit.read.
type Permission struct {
	Action      string `json:"action"`
	Description string `json:"description"`
}

// RoleBinding associates a role with a user or group.
type RoleBinding struct {
	ID          string    `json:"id"`
	RoleID      string    `json:"role_id"`
	RoleName    string    `json:"role_name"`
	SubjectType string    `json:"subject_type"` // user | group
	SubjectID   string    `json:"subject_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// Environment classifies servers (production, staging, ...).
type Environment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Critical    bool      `json:"critical"`
	CreatedAt   time.Time `json:"created_at"`
}

// ServerGroup groups destinations.
type ServerGroup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// Server is a registered SSH destination.
type Server struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Hostname           string     `json:"hostname"`
	Address            string     `json:"address"`
	Port               int        `json:"port"`
	EnvironmentID      string     `json:"environment_id"`
	Environment        string     `json:"environment,omitempty"`
	OSFamily           string     `json:"os_family"`
	Location           string     `json:"location"`
	Tags               []string   `json:"tags"`
	DefaultAccount     string     `json:"default_account"`
	HostKeyFingerprint string     `json:"host_key_fingerprint,omitempty"`
	Enabled            bool       `json:"enabled"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
}

// TimeWindow is an allowed access window in a named IANA timezone.
type TimeWindow struct {
	Days     []string `json:"days"`     // mon, tue, ...
	Start    string   `json:"start"`    // HH:MM
	End      string   `json:"end"`      // HH:MM
	Timezone string   `json:"timezone"` // IANA name
}

// AccessPolicy binds subjects to allowed/denied actions and resources.
type AccessPolicy struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	Description        string       `json:"description"`
	Priority           int          `json:"priority"`
	Effect             string       `json:"effect"`
	Subjects           []string     `json:"subjects"` // user:<id>, group:<id>, role:<name>, *
	Actions            []string     `json:"actions"`  // ssh.connect, * ...
	ServerIDs          []string     `json:"server_ids"`
	ServerGroupIDs     []string     `json:"server_group_ids"`
	EnvironmentIDs     []string     `json:"environment_ids"`
	Accounts           []string     `json:"accounts"`
	AllowedChannels    []string     `json:"allowed_channels"`
	RequireApproval    bool         `json:"require_approval"`
	ApprovalsRequired  int          `json:"approvals_required"`
	RequireMFA         bool         `json:"require_mfa"`
	MaxDurationSeconds int          `json:"max_duration_seconds"`
	TimeWindows        []TimeWindow `json:"time_windows"`
	Enabled            bool         `json:"enabled"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

// AccessRequest is a just-in-time access grant request.
type AccessRequest struct {
	ID                string     `json:"id"`
	RequesterID       string     `json:"requester_id"`
	RequesterName     string     `json:"requester_name,omitempty"`
	ServerID          string     `json:"server_id"`
	ServerName        string     `json:"server_name,omitempty"`
	ServerGroupID     string     `json:"server_group_id,omitempty"`
	EnvironmentID     string     `json:"environment_id,omitempty"`
	Account           string     `json:"account"`
	Reason            string     `json:"reason"`
	Ticket            string     `json:"ticket,omitempty"`
	RequestedSeconds  int        `json:"requested_seconds"`
	Status            string     `json:"status"`
	StartsAt          *time.Time `json:"starts_at,omitempty"`
	EndsAt            *time.Time `json:"ends_at,omitempty"`
	ApprovalsRequired int        `json:"approvals_required"`
	ApprovalsCount    int        `json:"approvals_count"`
	CreatedAt         time.Time  `json:"created_at"`
	DecidedAt         *time.Time `json:"decided_at,omitempty"`
}

// AccessApproval is one approver's decision.
type AccessApproval struct {
	ID           string    `json:"id"`
	RequestID    string    `json:"request_id"`
	ApproverID   string    `json:"approver_id"`
	ApproverName string    `json:"approver_name,omitempty"`
	Decision     string    `json:"decision"`
	Comment      string    `json:"comment"`
	CreatedAt    time.Time `json:"created_at"`
}

// Certificate is a persisted user certificate record (public material only).
type Certificate struct {
	ID                   string     `json:"id"`
	Serial               uint64     `json:"serial"`
	UserID               string     `json:"user_id"`
	Username             string     `json:"username,omitempty"`
	ServerID             string     `json:"server_id"`
	ServerName           string     `json:"server_name,omitempty"`
	Account              string     `json:"account"`
	KeyID                string     `json:"key_id"`
	PublicKeyFingerprint string     `json:"public_key_fingerprint"`
	CertPEM              string     `json:"cert_pem"`
	CA                   string     `json:"ca"`
	ValidAfter           time.Time  `json:"valid_after"`
	ValidBefore          time.Time  `json:"valid_before"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	RevokedReason        string     `json:"revoked_reason,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

// Session is one proxied SSH session.
type Session struct {
	ID                 string     `json:"id"`
	UserID             string     `json:"user_id"`
	Username           string     `json:"username,omitempty"`
	ServerID           string     `json:"server_id"`
	ServerName         string     `json:"server_name,omitempty"`
	Account            string     `json:"account"`
	SourceIP           string     `json:"source_ip"`
	AuthMethod         string     `json:"auth_method"`
	CertificateID      string     `json:"certificate_id,omitempty"`
	Gateway            string     `json:"gateway"`
	Status             string     `json:"status"`
	Command            string     `json:"command,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	DurationSeconds    int        `json:"duration_seconds"`
	TerminationReason  string     `json:"termination_reason,omitempty"`
	ExitStatus         *int       `json:"exit_status,omitempty"`
	RecordingID        string     `json:"recording_id,omitempty"`
	TerminateRequested bool       `json:"terminate_requested"`
}

// Recording is a terminal capture object.
type Recording struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Backend   string    `json:"backend"`
	ObjectKey string    `json:"object_key"`
	Format    string    `json:"format"`
	SizeBytes int64     `json:"size_bytes"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditEvent is a hash-chained audit record.
type AuditEvent struct {
	ID            int64          `json:"id"`
	EventID       string         `json:"event_id"`
	Category      string         `json:"category"`
	Action        string         `json:"action"`
	ActorID       string         `json:"actor_id,omitempty"`
	ActorName     string         `json:"actor_name"`
	Result        string         `json:"result"`
	ResourceType  string         `json:"resource_type,omitempty"`
	ResourceID    string         `json:"resource_id,omitempty"`
	SourceIP      string         `json:"source_ip,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	PrevHash      string         `json:"prev_hash"`
	Hash          string         `json:"hash"`
	CreatedAt     time.Time      `json:"created_at"`
}

// APIToken is an opaque bearer token digest.
type APIToken struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Kind      string     `json:"kind"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	LastUsed  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// UserPublicKey is a registered SSH public key (never private material).
type UserPublicKey struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	Name        string     `json:"name"`
	PublicKey   string     `json:"public_key"`
	Fingerprint string     `json:"fingerprint"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

// Principal is the authorization subject loaded for a request.
type Principal struct {
	UserID      string
	Username    string
	GroupIDs    []string
	Roles       []string
	Permissions map[string]bool
	MFA         bool
	Active      bool
}

// HasPermission reports whether the principal holds a capability, honouring
// exact matches and a trailing wildcard (for example "server.*").
func (p Principal) HasPermission(action string) bool {
	if p.Permissions["*"] || p.Permissions[action] {
		return true
	}
	for perm := range p.Permissions {
		if len(perm) > 1 && perm[len(perm)-1] == '*' {
			prefix := perm[:len(perm)-1]
			if len(action) >= len(prefix) && action[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}
