package authority

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

func TestLinuxCloneAuthorityDTOValidationAndClone(t *testing.T) {
	intent := testLinuxCloneIntent()
	if err := intent.Validate(); err != nil {
		t.Fatalf("intent.Validate() error = %v", err)
	}
	seed := LinuxCloneSeedReceipt{
		Version: LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("d", 64), Size: 128, Device: 7, Inode: 11,
	}
	if err := seed.Validate(); err != nil {
		t.Fatalf("seed.Validate() error = %v", err)
	}
	grant := LinuxCloneGrant{Intent: intent, Seed: seed}
	if err := grant.Validate(); err != nil {
		t.Fatalf("grant.Validate() error = %v", err)
	}
	clonedGrant := grant.Clone()
	if !grant.Equal(clonedGrant) || !grant.Intent.Equal(intent.Clone()) || !grant.Seed.Equal(seed.Clone()) {
		t.Fatal("clone DTO Clone()/Equal() did not preserve structural value equality")
	}

	checkout := LinuxCloneCheckoutReceipt{
		Version: LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target,
		Nonce: intent.Nonce, SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit,
		TargetDevice: 19, TargetInode: 23,
	}
	if err := checkout.Validate(); err != nil {
		t.Fatalf("checkout.Validate() error = %v", err)
	}
	if !checkout.Equal(checkout.Clone()) {
		t.Fatal("checkout Clone()/Equal() changed its structural value")
	}

	promotionRef, err := LinuxClonePromotionRef(intent.RunID, intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	promotion := LinuxClonePromotionReceipt{
		Version: LinuxCloneVersion, Commit: strings.Repeat("e", 40),
		TreeDigest: "sha256:" + strings.Repeat("f", 64), Ref: promotionRef,
	}
	if err := promotion.Validate(); err != nil {
		t.Fatalf("promotion.Validate() error = %v", err)
	}
	if !promotion.Equal(promotion.Clone()) {
		t.Fatal("promotion Clone()/Equal() changed its structural value")
	}
}

func TestLinuxCloneAuthorityRejectsMalformedAndAliasedEvidence(t *testing.T) {
	base := testLinuxCloneIntent()
	for _, test := range []struct {
		name   string
		mutate func(*LinuxCloneIntent)
	}{
		{name: "wrong version", mutate: func(value *LinuxCloneIntent) { value.Version = 1 }},
		{name: "unsafe run id", mutate: func(value *LinuxCloneIntent) { value.RunID = "../run" }},
		{name: "zero generation", mutate: func(value *LinuxCloneIntent) { value.Generation = 0 }},
		{name: "malformed resource ID", mutate: func(value *LinuxCloneIntent) { value.ResourceID = "repo" }},
		{name: "short nonce", mutate: func(value *LinuxCloneIntent) { value.Nonce = strings.Repeat("a", 32) }},
		{name: "uppercase nonce", mutate: func(value *LinuxCloneIntent) { value.Nonce = strings.Repeat("A", 64) }},
		{name: "partial commit", mutate: func(value *LinuxCloneIntent) { value.BaseCommit = "abc123" }},
		{name: "uppercase commit", mutate: func(value *LinuxCloneIntent) { value.BaseCommit = strings.Repeat("A", 40) }},
		{name: "malformed tree digest", mutate: func(value *LinuxCloneIntent) { value.BaseTreeDigest = strings.Repeat("a", 64) }},
		{name: "noncanonical seed path", mutate: func(value *LinuxCloneIntent) { value.SeedPath = "/tmp/../seed.bundle" }},
		{name: "seed and target overlap", mutate: func(value *LinuxCloneIntent) {
			value.SeedPath = "/var/lib/symmetry/seeds/run"
			value.Target = "/var/lib/symmetry/seeds/run/checkout"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := base.Clone()
			test.mutate(&value)
			if value.Validate() == nil {
				t.Fatal("Validate() accepted malformed clone intent")
			}
		})
	}

	seed := LinuxCloneSeedReceipt{
		Version: LinuxCloneVersion, BaseCommit: base.BaseCommit,
		Digest: "sha256:" + strings.Repeat("d", 64), Size: 1, Inode: 1,
	}
	for _, test := range []struct {
		name   string
		mutate func(*LinuxCloneSeedReceipt)
	}{
		{name: "raw digest", mutate: func(value *LinuxCloneSeedReceipt) { value.Digest = strings.Repeat("d", 64) }},
		{name: "zero size", mutate: func(value *LinuxCloneSeedReceipt) { value.Size = 0 }},
		{name: "oversized bundle", mutate: func(value *LinuxCloneSeedReceipt) { value.Size = LinuxCloneMaxSeedBytes + 1 }},
		{name: "zero inode", mutate: func(value *LinuxCloneSeedReceipt) { value.Inode = 0 }},
	} {
		t.Run("seed "+test.name, func(t *testing.T) {
			value := seed
			test.mutate(&value)
			if value.Validate() == nil {
				t.Fatal("Validate() accepted malformed seed receipt")
			}
		})
	}
	if (LinuxCloneGrant{Intent: base, Seed: seed}).Validate() != nil {
		t.Fatal("matching clone grant rejected")
	}
	changedSeed := seed
	changedSeed.BaseCommit = strings.Repeat("c", 40)
	if (LinuxCloneGrant{Intent: base, Seed: changedSeed}).Validate() == nil {
		t.Fatal("grant accepted a seed receipt for a different base commit")
	}

	checkout := LinuxCloneCheckoutReceipt{
		Version: LinuxCloneVersion, RunID: base.RunID, Generation: base.Generation,
		BindingKey: base.BindingKey, ResourceID: base.ResourceID, Target: base.Target, Nonce: base.Nonce,
		SeedDigest: seed.Digest, BaseCommit: base.BaseCommit, TargetInode: 3,
	}
	if err := checkout.Validate(); err != nil {
		t.Fatalf("valid checkout receipt rejected: %v", err)
	}
	checkout.Target = "/var/lib/symmetry/../other-run"
	if checkout.Validate() == nil {
		t.Fatal("checkout receipt accepted a noncanonical target path")
	}
}

func TestLinuxCloneRunRefsBindRunKeyAndUseBranchNamespace(t *testing.T) {
	runID := "run-123"
	base, err := LinuxCloneBaseRef(runID, 7)
	if err != nil {
		t.Fatal(err)
	}
	promotion, err := LinuxClonePromotionRef(runID, 7)
	if err != nil {
		t.Fatal(err)
	}
	expectedDigest := sha256.Sum256([]byte(runID + "\x00" + strconv.FormatInt(7, 10)))
	expectedHash := hex.EncodeToString(expectedDigest[:])
	if base != "refs/heads/symmetry/clone-v2/base/"+expectedHash {
		t.Fatalf("LinuxCloneBaseRef() = %q", base)
	}
	if promotion != "refs/heads/symmetry/clone-v2/promoted/"+expectedHash {
		t.Fatalf("LinuxClonePromotionRef() = %q", promotion)
	}
	if strings.HasPrefix(base, "refs/heads/") == false || strings.HasPrefix(promotion, "refs/heads/") == false {
		t.Fatal("clone refs are not branch refs usable by git clone --branch")
	}
	other, err := LinuxCloneBaseRef(runID, 8)
	if err != nil || other == base {
		t.Fatalf("base ref failed to bind generation: ref=%q err=%v", other, err)
	}
	if _, err := LinuxCloneBaseRef("../unsafe", 7); err == nil {
		t.Fatal("LinuxCloneBaseRef() accepted a path-like run ID")
	}
	if _, err := LinuxClonePromotionRef(runID, 0); err == nil {
		t.Fatal("LinuxClonePromotionRef() accepted zero generation")
	}
	invalidPromotion := LinuxClonePromotionReceipt{Version: LinuxCloneVersion, Commit: strings.Repeat("a", 40), TreeDigest: "sha256:" + strings.Repeat("b", 64), Ref: base}
	if invalidPromotion.Validate() == nil {
		t.Fatal("promotion receipt accepted a base ref instead of the promotion namespace")
	}
}

func TestLinuxCloneAbortProofBindsAllNoTargetEvidence(t *testing.T) {
	intent := testLinuxCloneIntent()
	baseRef, err := LinuxCloneBaseRef(intent.RunID, intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	promotionRef, err := LinuxClonePromotionRef(intent.RunID, intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	proof := LinuxCloneAbortProof{
		Version: LinuxCloneVersion, Intent: intent, BaseRef: baseRef, PromotionRef: promotionRef,
		TargetAbsent: true, SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("abort proof.Validate() error = %v", err)
	}
	cloned := proof.Clone()
	if !proof.Equal(cloned) {
		t.Fatal("abort proof Clone()/Equal() changed the no-target evidence")
	}
	seed := LinuxCloneSeedReceipt{
		Version: LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("c", 64), Size: 7, Inode: 9,
	}
	proof.SeedReceipt = &seed
	if err := proof.Validate(); err != nil {
		t.Fatalf("abort proof with bound seed Validate() error = %v", err)
	}
	cloned = proof.Clone()
	if !proof.Equal(cloned) || cloned.SeedReceipt == proof.SeedReceipt {
		t.Fatal("abort proof Clone() did not independently preserve optional seed identity")
	}
	changedSeed := *cloned.SeedReceipt
	changedSeed.Digest = "sha256:" + strings.Repeat("d", 64)
	cloned.SeedReceipt = &changedSeed
	if proof.Equal(cloned) {
		t.Fatal("abort proof Equal() ignored a changed seed digest")
	}

	for _, mutate := range []func(*LinuxCloneAbortProof){
		func(value *LinuxCloneAbortProof) { value.TargetAbsent = false },
		func(value *LinuxCloneAbortProof) { value.SeedAbsent = false },
		func(value *LinuxCloneAbortProof) { value.BaseRef = promotionRef },
		func(value *LinuxCloneAbortProof) { value.PromotionRefAbsent = false },
		func(value *LinuxCloneAbortProof) {
			value.PreGateReleaseProof = &LinuxCgroupPreGateReleaseProof{}
			value.CgroupAbortProof = &LinuxCgroupReservationAbortProof{}
		},
	} {
		candidate := proof.Clone()
		mutate(&candidate)
		if candidate.Validate() == nil {
			t.Fatal("Validate() accepted incomplete or contradictory no-target evidence")
		}
	}
}

func testLinuxCloneIntent() LinuxCloneIntent {
	return LinuxCloneIntent{
		Version: LinuxCloneVersion, RunID: "run-1", Generation: 3, BindingKey: "primary",
		ResourceID: "00000000-0000-4000-8000-000000000001",
		BaseCommit: strings.Repeat("a", 40), BaseTreeDigest: "sha256:" + strings.Repeat("b", 64),
		Nonce:    strings.Repeat("c", LinuxCloneNonceBytes*2),
		SeedPath: "/var/lib/symmetry/seeds/run-1.bundle",
		Target:   "/var/lib/symmetry/checkouts/binding-primary/run-run-1/generation-3",
	}
}
