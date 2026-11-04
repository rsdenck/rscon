package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/rsdenck/rscon/internal/authz"
	"github.com/rsdenck/rscon/internal/database"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/store"
)

// testStore connects to the database named by RSCON_TEST_DATABASE_URL and
// resets user data. The dedicated test database must already be migrated or
// migrations are applied automatically.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("RSCON_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("RSCON_TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, dsn, 8, 1, 10*time.Second)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(db.Close)
	if err := database.Migrate(ctx, db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	_, err = db.Pool.Exec(ctx, `TRUNCATE
		access_approvals, access_requests, session_recordings, sessions,
		ssh_certificates, api_tokens, user_public_keys, user_groups,
		role_bindings, server_group_members, access_policies, servers,
		server_groups, groups, users, authentication_events, audit_events,
		identity_providers
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset test database: %v", err)
	}
	return store.New(db)
}

func newUser(t *testing.T, st *store.Store, prefix string) *model.User {
	t.Helper()
	u := &model.User{
		ID:          identity.NewID(),
		Username:    fmt.Sprintf("%s-%s", prefix, identity.NewID()[:8]),
		DisplayName: prefix,
		Status:      model.StatusActive,
	}
	if err := st.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func newServer(t *testing.T, st *store.Store, name string) *model.Server {
	t.Helper()
	srv := &model.Server{
		ID:            identity.NewID(),
		Name:          name,
		Hostname:      name + ".example.test",
		Address:       "192.0.2.10",
		Port:          22,
		EnvironmentID: "env-production",
		OSFamily:      "linux",
		Enabled:       true,
	}
	if err := st.CreateServer(context.Background(), srv, nil); err != nil {
		t.Fatalf("create server: %v", err)
	}
	return srv
}

func TestUserLifecycleAndTokens(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	u := newUser(t, st, "alice")

	got, err := st.GetUserByUsername(ctx, u.Username)
	if err != nil {
		t.Fatalf("get user by username: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("unexpected user id %q", got.ID)
	}
	if _, err := st.GetUserByUsername(ctx, "ALICE-NOT-REAL"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected not found for unknown user, got %v", err)
	}
	if err := st.UpdateUserProfile(ctx, u.ID, "alice@example.test", "Alice Example"); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	got, err = st.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got.Email != "alice@example.test" || got.DisplayName != "Alice Example" {
		t.Fatalf("profile not updated: %+v", got)
	}

	token := &model.APIToken{
		ID: identity.NewID(), UserID: u.ID, Kind: model.TokenAPI,
		TokenHash: "hash-" + identity.NewID(), ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := st.CreateToken(ctx, token); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := st.GetTokenByHash(ctx, token.TokenHash); err != nil {
		t.Fatalf("get token: %v", err)
	}
	tokens, err := st.ListTokensForUser(ctx, u.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("list tokens: %v, %d", err, len(tokens))
	}
	if tokens[0].TokenHash != "" {
		t.Fatal("token digest must not be returned by ListTokensForUser")
	}

	if err := st.SetUserStatus(ctx, u.ID, model.StatusDisabled, true); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if _, err := st.GetTokenByHash(ctx, token.TokenHash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("disabling a user must revoke tokens, got %v", err)
	}
	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := st.GetUserByID(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("soft-deleted user must not be readable, got %v", err)
	}
}

func TestAccessRequestApprovalWorkflow(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	requester := newUser(t, st, "requester")
	approver := newUser(t, st, "approver")
	srv := newServer(t, st, "prod-db-01")

	role, err := st.GetRoleByName(ctx, "linux_administrator")
	if err != nil {
		t.Fatalf("load role: %v", err)
	}
	if err := st.BindRole(ctx, &model.RoleBinding{
		ID: identity.NewID(), RoleID: role.ID, SubjectType: "user", SubjectID: requester.ID,
	}); err != nil {
		t.Fatalf("bind role: %v", err)
	}

	policy := &model.AccessPolicy{
		ID: identity.NewID(), Name: "jit-prod-db", Priority: 10, Effect: model.EffectAllow,
		Subjects: []string{"user:" + requester.ID}, Actions: []string{authz.ActionSSHConnect},
		ServerIDs: []string{srv.ID}, Accounts: []string{"postgres"},
		RequireApproval: true, ApprovalsRequired: 1, Enabled: true,
	}
	if err := st.CreateAccessPolicy(ctx, policy); err != nil {
		t.Fatalf("create policy: %v", err)
	}

	svc := authz.New(st, nil)
	user, err := st.GetUser(ctx, requester.ID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	principal, err := svc.LoadPrincipal(ctx, user, false)
	if err != nil {
		t.Fatalf("load principal: %v", err)
	}
	decision, err := svc.Authorize(ctx, principal, authz.ActionSSHConnect, authz.Resource{
		Type: "server", ID: srv.ID, EnvironmentID: srv.EnvironmentID, Account: "postgres",
	}, authz.Options{})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if decision.Allowed {
		t.Fatal("access must be denied before a grant exists")
	}

	req := &model.AccessRequest{
		ID: identity.NewID(), RequesterID: requester.ID, ServerID: srv.ID,
		Account: "postgres", Reason: "rotate credentials", RequestedSeconds: 3600,
		ApprovalsRequired: 1,
	}
	if err := st.CreateAccessRequest(ctx, req); err != nil {
		t.Fatalf("create access request: %v", err)
	}
	if _, err := st.DecideAccessRequest(ctx, req.ID, requester.ID, model.DecisionApprove, "", false); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("self approval must be forbidden, got %v", err)
	}
	approved, err := st.DecideAccessRequest(ctx, req.ID, approver.ID, model.DecisionApprove, "looks good", false)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != model.RequestApproved || approved.StartsAt == nil || approved.EndsAt == nil {
		t.Fatalf("request not approved correctly: %+v", approved)
	}
	approvals, err := st.ListAccessApprovals(ctx, req.ID)
	if err != nil || len(approvals) != 1 || approvals[0].ApproverID != approver.ID {
		t.Fatalf("approvals not recorded: %v, %+v", err, approvals)
	}

	decision, err = svc.Authorize(ctx, principal, authz.ActionSSHConnect, authz.Resource{
		Type: "server", ID: srv.ID, EnvironmentID: srv.EnvironmentID, Account: "postgres",
	}, authz.Options{})
	if err != nil {
		t.Fatalf("authorize after approval: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("access should be allowed after approval: %s", decision.Reason)
	}
	decision, err = svc.Authorize(ctx, principal, authz.ActionSSHConnect, authz.Resource{
		Type: "server", ID: srv.ID, EnvironmentID: srv.EnvironmentID, Account: "root",
	}, authz.Options{})
	if err != nil {
		t.Fatalf("authorize other account: %v", err)
	}
	if decision.Allowed {
		t.Fatal("grant for postgres must not allow root")
	}

	if n, err := st.ExpireAccessRequests(ctx); err != nil || n != 0 {
		t.Fatalf("unexpected live grant expiry: %v, %d", err, n)
	}
}

func TestAuditChainDetection(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		e := &model.AuditEvent{
			EventID: identity.NewID(), Category: "security", Action: "test.event",
			ActorName: "tester", Result: "success", Message: fmt.Sprintf("event %d", i),
			Details: map[string]any{"index": i},
		}
		if err := st.AppendAudit(ctx, e); err != nil {
			t.Fatalf("append audit %d: %v", i, err)
		}
		if e.PrevHash == "" || e.Hash == "" {
			t.Fatalf("audit hashes not populated: %+v", e)
		}
	}
	verification, err := st.VerifyAuditChain(ctx)
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	if !verification.Valid || verification.Events != 3 {
		t.Fatalf("unexpected verification result: %+v", verification)
	}
	if _, err := st.Pool().Exec(ctx, `UPDATE audit_events SET message='tampered' WHERE id=(SELECT min(id) FROM audit_events)`); err != nil {
		t.Fatalf("tamper audit event: %v", err)
	}
	verification, err = st.VerifyAuditChain(ctx)
	if err != nil {
		t.Fatalf("verify tampered chain: %v", err)
	}
	if verification.Valid {
		t.Fatal("tampered audit chain must fail verification")
	}
}

func TestCertificateSessionAndRecording(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	u := newUser(t, st, "operator")
	srv := newServer(t, st, "prod-app-01")

	sum := sha256.Sum256([]byte("public-key"))
	cert := &model.Certificate{
		ID: identity.NewID(), Serial: 42, UserID: u.ID, ServerID: srv.ID, Account: "deploy",
		KeyID: "rscon:" + u.ID, PublicKeyFingerprint: hex.EncodeToString(sum[:]),
		CertPEM: "ssh-ed25519-cert-v01@openssh.com AAAA test", CA: "rscon-test",
		ValidAfter: time.Now().Add(-time.Minute), ValidBefore: time.Now().Add(10 * time.Minute),
	}
	if err := st.CreateCertificate(ctx, cert); err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	gotCert, err := st.GetCertificateBySerial(ctx, cert.Serial)
	if err != nil || gotCert.ID != cert.ID {
		t.Fatalf("get certificate: %v, %+v", err, gotCert)
	}

	exit := 0
	sess := &model.Session{
		ID: identity.NewID(), UserID: u.ID, ServerID: srv.ID, Account: "deploy",
		SourceIP: "198.51.100.7", AuthMethod: "certificate", CertificateID: cert.ID,
		Gateway: "rscond-test", Command: "uptime",
	}
	if err := st.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if n, err := st.CountActiveSessionsForUser(ctx, u.ID); err != nil || n != 1 {
		t.Fatalf("active session count: %v, %d", err, n)
	}
	rec := &model.Recording{
		ID: identity.NewID(), SessionID: sess.ID, Backend: "filesystem",
		ObjectKey: "sessions/" + sess.ID + ".cast", Format: "asciicast-v2",
		SizeBytes: 512, SHA256: hex.EncodeToString(sum[:]),
	}
	if err := st.CreateRecording(ctx, rec); err != nil {
		t.Fatalf("create recording: %v", err)
	}
	if err := st.SetSessionRecording(ctx, sess.ID, rec.ID); err != nil {
		t.Fatalf("attach recording: %v", err)
	}
	if err := st.CloseSession(ctx, sess.ID, model.SessionClosed, "client exited", &exit, rec.ID); err != nil {
		t.Fatalf("close session: %v", err)
	}
	session, err := st.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.Status != model.SessionClosed || session.RecordingID != rec.ID || session.ExitStatus == nil || *session.ExitStatus != 0 {
		t.Fatalf("session not closed correctly: %+v", session)
	}
	if err := st.RequestTermination(ctx, sess.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("terminating a closed session must fail, got %v", err)
	}

	revoked, err := st.RevokeCertificate(ctx, cert.ID, "test revocation")
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("revoke certificate: %v, %+v", err, revoked)
	}
	serials, err := st.ListRevokedSerialNumbers(ctx, time.Now().Add(-time.Hour))
	if err != nil || len(serials) != 1 || serials[0] != cert.Serial {
		t.Fatalf("revoked serials: %v, %v", err, serials)
	}
}

func TestDashboardCounts(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	newUser(t, st, "counted")
	newServer(t, st, "counted-server")
	counts, err := st.CountDashboard(ctx)
	if err != nil {
		t.Fatalf("count dashboard: %v", err)
	}
	if counts.Users != 1 || counts.Servers != 1 || counts.ActiveSessions != 0 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
}
