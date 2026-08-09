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
