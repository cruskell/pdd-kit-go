package pdd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func signingFixture(t *testing.T) (string, Plan, string, string, Approval) {
	t.Helper()
	root, p := fixture(t)
	keys := t.TempDir()
	secret := filepath.Join(keys, "reviewer.pem")
	identity, err := GenerateSigningKey(secret, filepath.Join(keys, "public.pem"))
	if err != nil {
		t.Fatal(err)
	}
	policy := ReviewPolicy{Schema: 1, MaxAgeHours: 24, Reviewers: []AuthorizedReviewer{{Name: "maintainer", PublicKey: identity.PublicKey, Plans: []string{"default"}}}}
	path := filepath.Join(keys, "policy.json")
	if err = WriteJSON(path, policy); err != nil {
		t.Fatal(err)
	}
	digest, err := Digest(root, p)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := ApproveSignedWithPolicy(root, p, "maintainer", []string{"identity"}, digest, secret, path)
	if err != nil {
		t.Fatal(err)
	}
	return root, p, secret, path, approval
}
func TestSignedGateAuthenticatesAndPreservesReview(t *testing.T) {
	root, p, _, policy, a := signingFixture(t)
	if a.Signature == nil || a.Signature.Algorithm != "ed25519" {
		t.Fatal("missing signature")
	}
	if _, err := ExportApprovalWithPolicy(root, p, ".pdd/reviews/signed.json", policy); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ApprovalPath)); err != nil {
		t.Fatal(err)
	}
	imported, err := ImportApprovalWithPolicy(root, p, ".pdd/reviews/signed.json", policy)
	if err != nil || !sameApproval(a, imported) {
		t.Fatal("signed assertion not preserved", err)
	}
	runner := &fakeRunner{}
	if _, _, err = Prove(context.Background(), root, p, Options{Runner: runner}); err == nil || runner.calls != 0 {
		t.Fatal("signed approval downgraded without policy")
	}
	r, _, err := Prove(context.Background(), root, p, Options{Runner: runner, PolicyPath: policy})
	if err != nil || !r.Success || r.ApprovalPolicy == nil {
		t.Fatalf("signed proof: %v %v", err, r.Gaps)
	}
	if _, _, err = Status(root, p); err == nil {
		t.Fatal("strict receipt accepted without rechecking authority")
	}
	if _, _, err = StatusWithPolicy(root, p, policy); err != nil {
		t.Fatal(err)
	}
	var config ReviewPolicy
	if err = ReadJSON(policy, &config); err != nil {
		t.Fatal(err)
	}
	config.Reviewers[0].Plans = []string{"other"}
	if err = WriteJSON(policy, config); err != nil {
		t.Fatal(err)
	}
	if _, _, err = StatusWithPolicy(root, p, policy); err == nil {
		t.Fatal("revoked authority still current")
	}
}
func TestPolicyRejectsForgeryExpiryAndWrongAuthority(t *testing.T) {
	root, p, secret, path, original := signingFixture(t)
	for _, change := range []func(*Approval){
		func(a *Approval) { a.Reviewer = "impersonated" },
		func(a *Approval) { a.AcceptedTrust = nil },
		func(a *Approval) { a.ApprovedAt = a.ApprovedAt.Add(-time.Hour) },
		func(a *Approval) { a.Signature = nil },
		func(a *Approval) { sig := *a.Signature; sig.Value = strings.Repeat("A", 88); a.Signature = &sig },
	} {
		a := original
		change(&a)
		if _, err := verifyPolicy(path, p, a, time.Now().UTC()); err == nil {
			t.Fatal("forged assertion authorized")
		}
	}
	for _, age := range []time.Duration{-25 * time.Hour, 10 * time.Minute} {
		a := original
		a.ApprovedAt = time.Now().UTC().Add(age)
		if err := signApproval(&a, secret); err != nil {
			t.Fatal(err)
		}
		if _, err := verifyPolicy(path, p, a, time.Now().UTC()); err == nil {
			t.Fatal("expired/future timestamp accepted")
		}
	}
	another := filepath.Join(t.TempDir(), "other.pem")
	if _, err := GenerateSigningKey(another, another+".pub"); err != nil {
		t.Fatal(err)
	}
	a := original
	if err := signApproval(&a, another); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyPolicy(path, p, a, time.Now().UTC()); err == nil {
		t.Fatal("unknown key impersonated reviewer")
	}
	if err := WriteJSON(filepath.Join(root, ".pdd/reviews/unknown.json"), a); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportApprovalWithPolicy(root, p, ".pdd/reviews/unknown.json", path); err == nil {
		t.Fatal("untrusted import accepted")
	}
	var active Approval
	if err := ReadJSON(filepath.Join(root, ApprovalPath), &active); err != nil || !sameApproval(active, original) {
		t.Fatal("failed import replaced active approval", err)
	}
}

type policyChangingRunner struct {
	delegate fakeRunner
	path     string
	changed  bool
}

func (r *policyChangingRunner) Run(ctx context.Context, root string, c Check) Execution {
	if !r.changed {
		r.changed = true
		_ = WriteJSON(r.path, ReviewPolicy{Schema: 1, MaxAgeHours: 24})
	}
	return r.delegate.Run(ctx, root, c)
}
func TestPolicyChangeDuringVerificationCannotProduceGreen(t *testing.T) {
	root, p, _, path, _ := signingFixture(t)
	r, _, err := Prove(context.Background(), root, p, Options{PolicyPath: path, Runner: &policyChangingRunner{path: path}})
	if err != nil || r.Success {
		t.Fatalf("authorization changed but proof green: %v %v", err, r.Gaps)
	}
	for _, v := range r.Requirements {
		if v.Status != "unproven" {
			t.Fatal("revoked run retained supported verdict")
		}
	}
}
func TestKeyGenerationNeverOverwritesOrReturnsSecret(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "private.pem")
	public := filepath.Join(dir, "public.pem")
	identity, err := GenerateSigningKey(secret, public)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(identity.PublicKey, "PRIVATE") || identity.KeyID == "" {
		t.Fatal("secret in public output")
	}
	if _, err = GenerateSigningKey(secret, public); err == nil {
		t.Fatal("key overwritten")
	}
	after, _ := os.ReadFile(secret)
	if string(before) != string(after) {
		t.Fatal("private key changed")
	}
	if err = checkPrivateKeyPermissions(secret); err != nil {
		t.Fatal(err)
	}
}
