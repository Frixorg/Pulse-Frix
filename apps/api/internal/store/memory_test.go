package store

import (
	"testing"
	"time"

	"github.com/frix-me/pulse/api/internal/model"
)

func TestTenantIsolation(t *testing.T) {
	m := NewMemory()
	orgA, _, _ := m.SeedOrgOwner("A", "a@example.com", "hashA")
	orgB, _, _ := m.SeedOrgOwner("B", "b@example.com", "hashB")

	srvA := &model.Server{ServerID: "srv_a", Hostname: "a-host"}
	srvB := &model.Server{ServerID: "srv_b", Hostname: "b-host"}
	_ = m.UpsertServer(orgA.ID, srvA)
	_ = m.UpsertServer(orgB.ID, srvB)

	// Org A must not be able to read Org B's server, even with the right id.
	if _, err := m.GetServer(orgA.ID, srvB.ID); err == nil {
		t.Fatal("cross-tenant read succeeded; isolation broken")
	}
	// Listing is scoped.
	listA, _ := m.ListServers(orgA.ID)
	if len(listA) != 1 || listA[0].ServerID != "srv_a" {
		t.Fatalf("org A should see exactly its own server, got %+v", listA)
	}
	// Deletion is scoped: A cannot delete B's server.
	if err := m.DeleteServer(orgA.ID, srvB.ID); err == nil {
		t.Fatal("cross-tenant delete succeeded; isolation broken")
	}
}

func TestEnrollmentTokenSingleUseAndExpiry(t *testing.T) {
	m := NewMemory()
	org, _, _ := m.SeedOrgOwner("A", "a@example.com", "h")
	now := time.Now()

	valid := &model.EnrollmentToken{OrgID: org.ID, TokenHash: "hash1", ExpiresAt: now.Add(time.Minute)}
	_ = m.CreateEnrollmentToken(valid)

	if _, err := m.ConsumeEnrollmentToken("hash1", now); err != nil {
		t.Fatalf("first use should succeed: %v", err)
	}
	if _, err := m.ConsumeEnrollmentToken("hash1", now); err == nil {
		t.Fatal("second use should fail (single-use)")
	}

	expired := &model.EnrollmentToken{OrgID: org.ID, TokenHash: "hash2", ExpiresAt: now.Add(-time.Minute)}
	_ = m.CreateEnrollmentToken(expired)
	if _, err := m.ConsumeEnrollmentToken("hash2", now); err == nil {
		t.Fatal("expired token should fail")
	}
}

func TestRevokedAgentNotReturned(t *testing.T) {
	m := NewMemory()
	org, _, _ := m.SeedOrgOwner("A", "a@example.com", "h")
	a := &model.Agent{OrgID: org.ID, ServerID: "srv", AgentID: "agt_1", PublicKey: "k"}
	_ = m.CreateAgent(a)
	if _, err := m.GetAgentByAgentID("agt_1"); err != nil {
		t.Fatalf("agent should be found: %v", err)
	}
	_ = m.RevokeAgent(org.ID, a.ID, time.Now())
	if _, err := m.GetAgentByAgentID("agt_1"); err == nil {
		t.Fatal("revoked agent must not be returned")
	}
}

func TestCountUsers(t *testing.T) {
	m := NewMemory()
	if n, err := m.CountUsers(); err != nil || n != 0 {
		t.Fatalf("a fresh store holds no accounts: got %d, %v", n, err)
	}
	if _, _, err := m.SeedOrgOwner("A", "a@example.com", "hash"); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if n, _ := m.CountUsers(); n != 1 {
		t.Fatalf("got %d", n)
	}
}

func TestUpdateUserEmail(t *testing.T) {
	m := NewMemory()
	org, user, _ := m.SeedOrgOwner("A", "old@example.com", "hash")

	if err := m.UpdateUserEmail(org.ID, user.ID, "new@example.com"); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if _, _, err := m.FindLoginByEmail("new@example.com"); err != nil {
		t.Error("the new address should resolve to the account")
	}
	if _, _, err := m.FindLoginByEmail("old@example.com"); err == nil {
		t.Error("the old address must stop resolving")
	}

	// A second account cannot take an address that is already in use.
	orgB, userB, _ := m.SeedOrgOwner("B", "b@example.com", "hash")
	if err := m.UpdateUserEmail(orgB.ID, userB.ID, "new@example.com"); err != ErrAlreadyExists {
		t.Errorf("expected ErrAlreadyExists, got %v", err)
	}
	// And no other tenant can touch this user.
	if err := m.UpdateUserEmail(orgB.ID, user.ID, "hijack@example.com"); err != ErrNotFound {
		t.Errorf("cross-tenant update should be ErrNotFound, got %v", err)
	}
}

func TestUpdateUserPasswordAndSessionRevocation(t *testing.T) {
	m := NewMemory()
	org, user, _ := m.SeedOrgOwner("A", "a@example.com", "old-hash")

	other := &model.Session{ID: "sess-1", UserID: user.ID, OrgID: org.ID, ExpiresAt: time.Now().Add(time.Hour)}
	current := &model.Session{ID: "sess-2", UserID: user.ID, OrgID: org.ID, ExpiresAt: time.Now().Add(time.Hour)}
	_ = m.CreateSession(other)
	_ = m.CreateSession(current)

	if err := m.UpdateUserPassword(org.ID, user.ID, "new-hash"); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	got, _, _ := m.FindLoginByEmail("a@example.com")
	if got.PasswordHash != "new-hash" {
		t.Errorf("password hash was not replaced: %q", got.PasswordHash)
	}

	// Every session dies with the old password.
	if err := m.DeleteSessionsForUser(user.ID); err != nil {
		t.Fatalf("revocation failed: %v", err)
	}
	for _, id := range []string{"sess-1", "sess-2"} {
		if _, err := m.GetSession(id); err == nil {
			t.Errorf("session %s survived the password change", id)
		}
	}
}
