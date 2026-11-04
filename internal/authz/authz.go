// Package authz implements RSCON's two-layer authorization model: RBAC
// capabilities plus resource-scoped access policies with deny precedence.
package authz

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rsdenck/rscon/internal/model"
)

// Actions are the capabilities checked by the engine.
const (
	ActionSSHConnect        = "ssh.connect"
	ActionCertificateIssue  = "certificate.issue"
	ActionCertificateRead   = "certificate.read"
	ActionCertificateRevoke = "certificate.revoke"
	ActionServerRead        = "server.read"
	ActionServerManage      = "server.manage"
	ActionUserRead          = "user.read"
	ActionUserManage        = "user.manage"
	ActionGroupRead         = "group.read"
	ActionGroupManage       = "group.manage"
	ActionRoleRead          = "role.read"
	ActionRoleManage        = "role.manage"
	ActionPolicyRead        = "policy.read"
	ActionPolicyManage      = "policy.manage"
	ActionAccessRequest     = "access.request"
	ActionAccessApprove     = "access.approve"
	ActionAccessRead        = "access.read"
	ActionSessionRead       = "session.read"
	ActionSessionTerminate  = "session.terminate"
	ActionRecordingRead     = "recording.read"
	ActionAuditRead         = "audit.read"
	ActionVaultRead         = "vault.read"
	ActionEnvironmentManage = "environment.manage"
)

// policyRequiredActions must match at least one explicit allow policy. Other
// actions only require the RBAC capability and are subject to deny policies.
var policyRequiredActions = map[string]bool{
	ActionSSHConnect:    true,
	ActionAccessRequest: true,
}

// Resource is the target of an authorization decision.
type Resource struct {
	Type          string
	ID            string
	Name          string
	EnvironmentID string
	GroupIDs      []string
	Account       string
	Channel       string
}

// Decision is the outcome of an authorization evaluation.
type Decision struct {
	Allowed           bool
	Reason            string
	MatchedPolicy     string
	ApprovalRequired  bool
	ApprovalsRequired int
	MFARequired       bool
	MaxDuration       time.Duration
	AllowedChannels   []string
}

// Options tune a decision without weakening deny rules.
type Options struct {
	// SkipApprovalCheck is used by the access-request endpoint to determine
	// whether approval is required without requiring an existing grant.
	SkipApprovalCheck bool
	// Channel is the SSH channel being authorized (session, sftp, tcpip).
	Channel string
	// Now overrides the clock for time-window evaluation.
	Now time.Time
}

// Repository supplies the data needed to evaluate a principal.
type Repository interface {
	GetUser(ctx context.Context, id string) (*model.User, error)
	ListUserGroupIDs(ctx context.Context, userID string) ([]string, error)
	ListBindingsForSubjects(ctx context.Context, userID string, groupIDs []string) ([]model.RoleBinding, error)
	ListPermissionsByRoleIDs(ctx context.Context, roleIDs []string) ([]string, error)
	ListAccessPolicies(ctx context.Context) ([]model.AccessPolicy, error)
	ListServerGroupIDs(ctx context.Context, serverID string) ([]string, error)
	HasActiveGrant(ctx context.Context, userID, serverID string, groupIDs []string, environmentID, account string, at time.Time) (bool, error)
}

// Service evaluates authorization decisions.
type Service struct {
	repo Repository
	now  func() time.Time
}

// New returns a policy service. If now is nil, time.Now is used.
func New(repo Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, now: now}
}

// LoadPrincipal resolves a user's groups, roles, permissions and status.
func (s *Service) LoadPrincipal(ctx context.Context, user *model.User, mfa bool) (model.Principal, error) {
	p := model.Principal{
		UserID:      user.ID,
		Username:    user.Username,
		MFA:         mfa || user.MFAEnrolled,
		Active:      user.Active(),
		Permissions: map[string]bool{},
	}
	groups, err := s.repo.ListUserGroupIDs(ctx, user.ID)
	if err != nil {
		return p, fmt.Errorf("load groups: %w", err)
	}
	p.GroupIDs = groups
	bindings, err := s.repo.ListBindingsForSubjects(ctx, user.ID, groups)
	if err != nil {
		return p, fmt.Errorf("load role bindings: %w", err)
	}
	seen := map[string]bool{}
	roleIDs := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if seen[b.RoleID] {
			continue
		}
		seen[b.RoleID] = true
		roleIDs = append(roleIDs, b.RoleID)
		p.Roles = append(p.Roles, b.RoleName)
	}
	perms, err := s.repo.ListPermissionsByRoleIDs(ctx, roleIDs)
	if err != nil {
		return p, fmt.Errorf("load permissions: %w", err)
	}
	for _, perm := range perms {
		p.Permissions[perm] = true
	}
	return p, nil
}

// Authorize evaluates an action against a resource.
func (s *Service) Authorize(ctx context.Context, p model.Principal, action string, res Resource, opts Options) (Decision, error) {
	now := opts.Now
	if now.IsZero() {
		now = s.now()
	}
	if opts.Channel != "" {
		res.Channel = opts.Channel
	}

	deny := func(reason string) (Decision, error) {
		return Decision{Allowed: false, Reason: reason}, nil
	}
	if !p.Active {
		return deny("user is not active")
	}
	if !p.HasPermission(action) {
		return deny("missing permission " + action)
	}

	policies, err := s.repo.ListAccessPolicies(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("load access policies: %w", err)
	}

	var matched []model.AccessPolicy
	for _, policy := range policies {
		if !policyMatches(policy, p, action, res, now) {
			continue
		}
		if policy.Effect == model.EffectDeny {
			return Decision{
				Allowed:       false,
				Reason:        "denied by policy " + policy.Name,
				MatchedPolicy: policy.Name,
			}, nil
		}
		matched = append(matched, policy)
	}

	if len(matched) == 0 {
		if policyRequiredActions[action] {
			return deny("no access policy allows this request")
		}
		return Decision{Allowed: true, Reason: "rbac capability granted"}, nil
	}

	slices.SortStableFunc(matched, func(a, b model.AccessPolicy) int {
		if a.Priority != b.Priority {
			return a.Priority - b.Priority
		}
		return specificity(b) - specificity(a)
	})

	decision := Decision{Allowed: true, MatchedPolicy: matched[0].Name}
	for _, policy := range matched {
		if policy.RequireApproval {
			decision.ApprovalRequired = true
			if policy.ApprovalsRequired > decision.ApprovalsRequired {
				decision.ApprovalsRequired = policy.ApprovalsRequired
			}
		}
		if policy.RequireMFA {
			decision.MFARequired = true
		}
		if policy.MaxDurationSeconds > 0 {
			d := time.Duration(policy.MaxDurationSeconds) * time.Second
			if decision.MaxDuration == 0 || d < decision.MaxDuration {
				decision.MaxDuration = d
			}
		}
		for _, ch := range policy.AllowedChannels {
			if !slices.Contains(decision.AllowedChannels, ch) {
				decision.AllowedChannels = append(decision.AllowedChannels, ch)
			}
		}
	}
	if decision.ApprovalsRequired == 0 && decision.ApprovalRequired {
		decision.ApprovalsRequired = 1
	}
	if decision.MFARequired && !p.MFA {
		decision.Allowed = false
		decision.Reason = "multi-factor authentication is required"
		return decision, nil
	}
	if decision.ApprovalRequired && !opts.SkipApprovalCheck {
		granted, err := s.repo.HasActiveGrant(ctx, p.UserID, res.ID,
			res.GroupIDs, res.EnvironmentID, res.Account, now)
		if err != nil {
			return Decision{}, fmt.Errorf("check access grant: %w", err)
		}
		if !granted {
			decision.Allowed = false
			decision.Reason = "an approved just-in-time access grant is required"
			return decision, nil
		}
	}
	if decision.Allowed && decision.MaxDuration > 0 {
		decision.Reason = fmt.Sprintf("allowed by %s (max %s)", decision.MatchedPolicy, decision.MaxDuration)
	} else if decision.Allowed {
		decision.Reason = "allowed by " + decision.MatchedPolicy
	}
	return decision, nil
}

func policyMatches(policy model.AccessPolicy, p model.Principal, action string, res Resource, now time.Time) bool {
	if !policy.Enabled || (policy.Effect != model.EffectAllow && policy.Effect != model.EffectDeny) {
		return false
	}
	if !matchSubjects(policy.Subjects, p) || !matchStringList(policy.Actions, action) {
		return false
	}
	if len(policy.Accounts) > 0 {
		if res.Account == "" || !matchStringList(policy.Accounts, res.Account) {
			return false
		}
	}
	if len(policy.ServerIDs) > 0 && !slices.Contains(policy.ServerIDs, res.ID) {
		return false
	}
	if len(policy.ServerGroupIDs) > 0 && !intersects(policy.ServerGroupIDs, res.GroupIDs) {
		return false
	}
	if len(policy.EnvironmentIDs) > 0 && !slices.Contains(policy.EnvironmentIDs, res.EnvironmentID) {
		return false
	}
	if len(policy.AllowedChannels) > 0 && res.Channel != "" && !matchStringList(policy.AllowedChannels, res.Channel) {
		return false
	}
	if len(policy.TimeWindows) > 0 {
		ok := false
		for _, w := range policy.TimeWindows {
			if windowContains(w, now) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func matchSubjects(subjects []string, p model.Principal) bool {
	if len(subjects) == 0 {
		return true
	}
	for _, s := range subjects {
		switch {
		case s == "*":
			return true
		case strings.HasPrefix(s, "user:") && strings.TrimPrefix(s, "user:") == p.UserID:
			return true
		case strings.HasPrefix(s, "group:") && slices.Contains(p.GroupIDs, strings.TrimPrefix(s, "group:")):
			return true
		case strings.HasPrefix(s, "role:") && slices.Contains(p.Roles, strings.TrimPrefix(s, "role:")):
			return true
		case !strings.Contains(s, ":") && slices.Contains(p.Roles, s):
			return true
		}
	}
	return false
}

func matchStringList(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if pattern == "*" || pattern == value {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(value, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func intersects(a, b []string) bool {
	for _, v := range a {
		if slices.Contains(b, v) {
			return true
		}
	}
	return false
}

func specificity(p model.AccessPolicy) int {
	score := 0
	if len(p.ServerIDs) > 0 {
		score += 8
	}
	if len(p.ServerGroupIDs) > 0 {
		score += 4
	}
	if len(p.EnvironmentIDs) > 0 {
		score += 2
	}
	if len(p.Accounts) > 0 {
		score += 2
	}
	if len(p.TimeWindows) > 0 {
		score++
	}
	return score
}

var dayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday,
	"wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

func windowContains(w model.TimeWindow, now time.Time) bool {
	loc := time.UTC
	if w.Timezone != "" {
		l, err := time.LoadLocation(w.Timezone)
		if err != nil {
			return false
		}
		loc = l
	}
	local := now.In(loc)
	day := strings.ToLower(local.Weekday().String()[:3])
	if len(w.Days) > 0 && !slices.Contains(w.Days, day) {
		return false
	}
	minutes := local.Hour()*60 + local.Minute()
	start, ok1 := parseClock(w.Start)
	end, ok2 := parseClock(w.End)
	if !ok1 || !ok2 {
		return false
	}
	if start <= end {
		return minutes >= start && minutes < end
	}
	// Window crosses midnight.
	return minutes >= start || minutes < end
}

func parseClock(v string) (int, bool) {
	parts := strings.Split(v, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// ValidatePolicy performs structural validation used by the API.
func ValidatePolicy(p *model.AccessPolicy) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("policy name is required")
	}
	if p.Effect != model.EffectAllow && p.Effect != model.EffectDeny {
		return fmt.Errorf("effect must be allow or deny")
	}
	if p.ApprovalsRequired < 0 {
		return fmt.Errorf("approvals_required must be >= 0")
	}
	if p.MaxDurationSeconds < 0 {
		return fmt.Errorf("max_duration_seconds must be >= 0")
	}
	for _, w := range p.TimeWindows {
		if _, ok := parseClock(w.Start); !ok {
			return fmt.Errorf("invalid time window start %q", w.Start)
		}
		if _, ok := parseClock(w.End); !ok {
			return fmt.Errorf("invalid time window end %q", w.End)
		}
		if w.Timezone != "" {
			if _, err := time.LoadLocation(w.Timezone); err != nil {
				return fmt.Errorf("invalid timezone %q", w.Timezone)
			}
		}
		for _, d := range w.Days {
			if _, ok := dayNames[strings.ToLower(d)]; !ok {
				return fmt.Errorf("invalid day %q", d)
			}
		}
	}
	return nil
}
