package pdd

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Signature covers the entire approval, including frozen review and timestamp.
// The public key establishes integrity; an independently supplied policy grants authority.
type ApprovalSignature struct {
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"`
	Value     string `json:"value"`
}
type AuthorizedReviewer struct {
	Name      string   `json:"name"`
	PublicKey string   `json:"public_key"`
	Plans     []string `json:"plans"`
}
type ReviewPolicy struct {
	Schema      int                  `json:"schema"`
	MaxAgeHours int                  `json:"max_age_hours"`
	Reviewers   []AuthorizedReviewer `json:"reviewers"`
}
type PolicyEvidence struct {
	Digest   string `json:"digest"`
	KeyID    string `json:"key_id"`
	Reviewer string `json:"reviewer"`
}
type PublicIdentity struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

func approvalMessage(a Approval) ([]byte, error) {
	a.Signature = nil
	data, err := json.Marshal(a)
	return append([]byte("pdd-kit-go/approval/ed25519/v1\x00"), data...), err
}
func publicKey(text string) (ed25519.PublicKey, error) {
	block, rest := pem.Decode([]byte(text))
	if block == nil || block.Type != "PUBLIC KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, fmt.Errorf("expected one Ed25519 PUBLIC KEY PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("review key must use Ed25519")
	}
	return key, nil
}
func verifySignature(a Approval) (string, error) {
	if a.Signature == nil {
		return "", fmt.Errorf("signed approval required")
	}
	if a.Signature.Algorithm != "ed25519" {
		return "", fmt.Errorf("unsupported approval signature algorithm")
	}
	key, err := publicKey(a.Signature.PublicKey)
	if err != nil {
		return "", err
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(a.Signature.Value)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return "", fmt.Errorf("invalid approval signature encoding")
	}
	message, err := approvalMessage(a)
	if err != nil {
		return "", err
	}
	if !ed25519.Verify(key, message, signature) {
		return "", fmt.Errorf("approval signature does not match its review, identity or timestamp")
	}
	return hashBytes(key), nil
}
func signApproval(a *Approval, path string) error {
	data, err := readArtifact(path, 16<<10)
	if err != nil {
		return err
	}
	if err = checkPrivateKeyPermissions(path); err != nil {
		return err
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return fmt.Errorf("expected one PKCS8 PRIVATE KEY PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("invalid signing key")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return fmt.Errorf("signing key must use Ed25519")
	}
	pub, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return err
	}
	message, err := approvalMessage(*a)
	if err != nil {
		return err
	}
	a.Signature = &ApprovalSignature{Algorithm: "ed25519", PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})), Value: base64.StdEncoding.EncodeToString(ed25519.Sign(key, message))}
	return nil
}

// GenerateSigningKey creates files exclusively and never prints private material.
func GenerateSigningKey(privatePath, publicPath string) (PublicIdentity, error) {
	var identity PublicIdentity
	if privatePath == "" || publicPath == "" {
		return identity, fmt.Errorf("both private and public key paths are required")
	}
	privatePath, _ = filepath.Abs(privatePath)
	publicPath, _ = filepath.Abs(publicPath)
	if privatePath == publicPath {
		return identity, fmt.Errorf("key paths must differ")
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return identity, err
	}
	secret, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return identity, err
	}
	public, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return identity, err
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})
	writeNew := func(path string, data []byte, mode os.FileMode) error {
		if err := NoSymlinks(path); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
		}
		return err
	}
	if err = writeNew(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret}), 0600); err != nil {
		return identity, err
	}
	if err = writeNew(publicPath, publicPEM, 0644); err != nil {
		_ = os.Remove(privatePath)
		return identity, err
	}
	return PublicIdentity{Algorithm: "ed25519", KeyID: hashBytes(pub), PublicKey: string(publicPEM)}, nil
}

func verifyPolicy(path string, p Plan, a Approval, now time.Time) (PolicyEvidence, error) {
	var policy ReviewPolicy
	if err := ReadJSON(path, &policy); err != nil {
		return PolicyEvidence{}, fmt.Errorf("review policy: %w", err)
	}
	if policy.Schema != 1 || policy.MaxAgeHours < 1 || policy.MaxAgeHours > 8760 || len(policy.Reviewers) == 0 {
		return PolicyEvidence{}, fmt.Errorf("review policy requires schema 1, max_age_hours 1..8760 and authorized reviewers")
	}
	keyID, err := verifySignature(a)
	if err != nil {
		return PolicyEvidence{}, err
	}
	if a.Review == nil {
		return PolicyEvidence{}, fmt.Errorf("policy requires complete frozen review")
	}
	if a.ApprovedAt.After(now.Add(5*time.Minute)) || now.Sub(a.ApprovedAt) > time.Duration(policy.MaxAgeHours)*time.Hour {
		return PolicyEvidence{}, fmt.Errorf("approval timestamp is in the future or expired")
	}
	seenNames := map[string]bool{}
	seenKeys := map[string]bool{}
	authorized := false
	for _, reviewer := range policy.Reviewers {
		key, err := publicKey(reviewer.PublicKey)
		if err != nil {
			return PolicyEvidence{}, fmt.Errorf("policy reviewer %q: %w", reviewer.Name, err)
		}
		id := hashBytes(key)
		if strings.TrimSpace(reviewer.Name) == "" || seenNames[reviewer.Name] || seenKeys[id] || len(reviewer.Plans) == 0 {
			return PolicyEvidence{}, fmt.Errorf("policy reviewer names/keys must be distinct with explicit allowed plans")
		}
		seenNames[reviewer.Name] = true
		seenKeys[id] = true
		seenPlans := map[string]bool{}
		for _, name := range reviewer.Plans {
			paths, err := PathsFor(name)
			if err != nil || paths.Name != name || seenPlans[name] {
				return PolicyEvidence{}, fmt.Errorf("invalid or duplicate policy plan %q", name)
			}
			seenPlans[name] = true
			if name == p.ID() && reviewer.Name == a.Reviewer && id == keyID {
				authorized = true
			}
		}
	}
	if !authorized {
		return PolicyEvidence{}, fmt.Errorf("signing key and reviewer are not authorized for plan %s (possibly revoked)", p.ID())
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return PolicyEvidence{}, err
	}
	return PolicyEvidence{Digest: hashBytes(encoded), KeyID: keyID, Reviewer: a.Reviewer}, nil
}

// Called under the project lock before installing a new active approval. Local
// draft mode must not silently remove the signature from a previously signed plan.
func protectSignedApproval(root string, p Plan, next Approval) error {
	if next.Signature != nil {
		return nil
	}
	var current Approval
	err := ReadJSON(filepath.Join(root, p.stateFile("approval.json")), &current)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot inspect existing approval before replacement: %w", err)
	}
	if current.Signature != nil {
		return fmt.Errorf("cannot replace a signed approval with an unsigned assertion; renew with --signing-key or import a signed review")
	}
	return nil
}
