package authz

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsdenck/rscon/internal/model"
)

type fakeRepo struct {
	user         *model.User
	groups       []string
	bindings     []model.RoleBinding
	permissions  []string
	policies     []model.AccessPolicy
	serverGroups []string
	grant        bool
}

func (f *fakeRepo) GetUser(context.Context, string) (*model.User, error)       { return f.user, nil }
func (f *fakeRepo) ListUserGroupIDs(context.Context, string) ([]string, error) { return f.groups, nil }
func (f *fakeRepo) ListBindingsForSubjects(context.Context, string, []string) ([]model.RoleBinding, error) {
	return f.bindings, nil
}
func (f *fakeRepo) ListPermissionsByRoleIDs(context.Context, []string) ([]string, error) {
	return f.permissions, nil
}
func (f *fakeRepo) ListAccessPolicies(context.Context) ([]model.AccessPolicy, error) {
	return f.policies, nil
}
func (f *fakeRepo) ListServerGroupIDs(context.Context, string) ([]string, error) {
	return f.serverGroups, nil
}
func (f *fakeRepo) HasActiveGrant(context.Context, string, string, []string, string, string, time.Time) (bool, error) {
	return f.grant, nil
}

func baseRepo() *fakeRepo {
	return &fakeRepo{
		user:        &model.User{ID: "u1", Username: "alice", Status: model.StatusActive},
		groups:      []string{"g1"},
		bindings:    []model.RoleBinding{{RoleID: "r1", RoleName: "platform_engineer"}},
		permissions: []string{ActionSSHConnect, ActionServerRead},
		policies: []model.AccessPolicy{{
			ID: "p1", Name: "dev-access", Priority: 100, Effect: model.EffectAllow,
			Subjects: []string{"user:u1"}, Actions: []string{ActionSSHConnect},
			EnvironmentIDs: []string{"env-dev"}, Accounts: []string{"deploy"},
			Enabled: true,
		}},
		serverGroups: []string{"sg1"},
	}
}

func principal(repo *fakeRepo) model.Principal {
	svc := New(repo, func() time.Time { return time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) })
	p, err := svc.LoadPrincipal(context.Background(), repo.user, false)
	if err != nil {
		panic(err)
	}
	return p
}

func res() Resource {
	return Resource{
		Type: "server", ID: "s1", Name: "web01",
		EnvironmentID: "env-dev", GroupIDs: []string{"sg1"}, Account: "deploy",
	}
}

func TestAllow(t *testing.T) {
	repo := baseRepo()
	svc := New(repo, nil)
	d, err := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed {
		t.Fatalf("expected allow, got %+v", d)
	}
}

func TestMissingPermissionDenied(t *testing.T) {
	repo := baseRepo()
	repo.permissions = []string{ActionServerRead}
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{})
	if d.Allowed {
		t.Fatal("expected deny without permission")
	}
}

func TestDefaultDenyWithoutPolicy(t *testing.T) {
	repo := baseRepo()
	repo.policies = nil
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{})
	if d.Allowed || !strings.Contains(d.Reason, "no access policy") {
		t.Fatalf("expected default deny, got %+v", d)
	}
}

func TestDenyOverridesAllow(t *testing.T) {
	repo := baseRepo()
	repo.policies = append(repo.policies, model.AccessPolicy{
		ID: "p2", Name: "prod-block", Priority: 1, Effect: model.EffectDeny,
		Actions: []string{ActionSSHConnect}, EnvironmentIDs: []string{"env-dev"},
		Enabled: true,
	})
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{})
	if d.Allowed || !strings.Contains(d.Reason, "denied by policy") {
		t.Fatalf("expected deny precedence, got %+v", d)
	}
}

func TestAccountRestriction(t *testing.T) {
	repo := baseRepo()
	r := res()
	r.Account = "root"
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, r, Options{})
	if d.Allowed {
		t.Fatal("expected deny for unlisted account")
	}
}

func TestEnvironmentRestriction(t *testing.T) {
	repo := baseRepo()
	r := res()
	r.EnvironmentID = "env-prod"
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, r, Options{})
	if d.Allowed {
		t.Fatal("expected deny for unlisted environment")
	}
}

func TestApprovalRequiredWithoutGrant(t *testing.T) {
	repo := baseRepo()
	repo.policies[0].RequireApproval = true
	repo.policies[0].ApprovalsRequired = 2
	repo.policies[0].MaxDurationSeconds = 3600
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{})
	if d.Allowed || !d.ApprovalRequired || d.ApprovalsRequired != 2 {
		t.Fatalf("expected approval requirement, got %+v", d)
	}

	d, _ = svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{SkipApprovalCheck: true})
	if !d.Allowed || !d.ApprovalRequired {
		t.Fatalf("expected allow when skipping grant check, got %+v", d)
	}

	repo.grant = true
	d, _ = svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{})
	if !d.Allowed || d.MaxDuration != time.Hour {
		t.Fatalf("expected allow with grant and max duration, got %+v", d)
	}
}

func TestMFARequired(t *testing.T) {
	repo := baseRepo()
	repo.policies[0].RequireMFA = true
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{})
	if d.Allowed || !d.MFARequired {
		t.Fatalf("expected MFA denial, got %+v", d)
	}

	p := principal(repo)
	p.MFA = true
	d, _ = svc.Authorize(context.Background(), p, ActionSSHConnect, res(), Options{})
	if !d.Allowed {
		t.Fatalf("expected MFA principal to be allowed, got %+v", d)
	}
}

func TestTimeWindow(t *testing.T) {
	repo := baseRepo()
	repo.policies[0].TimeWindows = []model.TimeWindow{{Days: []string{"tue"}, Start: "09:00", End: "17:00", Timezone: "UTC"}}
	svc := New(repo, nil)
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) // Tuesday
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{Now: at})
	if !d.Allowed {
		t.Fatalf("expected inside window, got %+v", d)
	}
	outside := time.Date(2026, 3, 10, 20, 0, 0, 0, time.UTC)
	d, _ = svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{Now: outside})
	if d.Allowed {
		t.Fatal("expected outside window denial")
	}
}

func TestChannelPolicy(t *testing.T) {
	repo := baseRepo()
	repo.policies[0].AllowedChannels = []string{"session"}
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{Channel: "tcpip"})
	if d.Allowed {
		t.Fatal("expected tcpip denial")
	}
	d, _ = svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{Channel: "session"})
	if !d.Allowed {
		t.Fatal("expected session allow")
	}
}

func TestInactiveUser(t *testing.T) {
	repo := baseRepo()
	repo.user.Status = model.StatusDisabled
	repo.permissions = []string{ActionSSHConnect}
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), principal(repo), ActionSSHConnect, res(), Options{})
	if d.Allowed {
		t.Fatal("expected disabled user denial")
	}
}

func TestReadActionWithRBACOnly(t *testing.T) {
	repo := baseRepo()
	p := principal(repo)
	p.Permissions[ActionServerRead] = true
	svc := New(repo, nil)
	d, _ := svc.Authorize(context.Background(), p, ActionServerRead, Resource{}, Options{})
	if !d.Allowed {
		t.Fatalf("expected RBAC read allow, got %+v", d)
	}
}
