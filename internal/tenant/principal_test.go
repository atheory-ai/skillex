package tenant

import "testing"

func TestPartitionsDoNotExposeOrCrossTenantPrincipalIdentity(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	a, err := Partition(key, Principal{TenantID: "tenant-a", Subject: "user-1", Kind: PrincipalUser})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Partition(key, Principal{TenantID: "tenant-b", Subject: "user-1", Kind: PrincipalUser})
	if err != nil {
		t.Fatal(err)
	}
	w, err := Partition(key, Principal{TenantID: "tenant-a", Subject: "user-1", Kind: PrincipalWorkload})
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a == w || a == "" {
		t.Fatalf("partitions were not isolated: %q %q %q", a, b, w)
	}
}

func TestSigningKeysAreTenantAndPurposeIsolated(t *testing.T) {
	master := []byte("01234567890123456789012345678901")
	a, _ := DeriveSigningKey(master, "tenant-a", "capability-refs")
	b, _ := DeriveSigningKey(master, "tenant-b", "capability-refs")
	tokens, _ := DeriveSigningKey(master, "tenant-a", "oauth-tokens")
	if string(a) == string(b) || string(a) == string(tokens) || len(a) != 32 {
		t.Fatal("tenant key derivation did not isolate tenants and purposes")
	}
}
