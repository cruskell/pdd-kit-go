# Prove one function in your Go project

Start with a small production decision. This example proves that an inventory
reservation is accepted exactly when the amount is positive and stock is
sufficient. It also runs a named Go test for representative cases.

## Create the example module

Use `pdd` v0.4.0, Go and Docker. Run this block in a new scratch directory.

```sh
go mod init example.com/inventory
cat > stock.go <<'GO'
package inventory

// @ ensures allowed == (requested > 0 && requested <= available)
func CanReserve(available, requested int) (allowed bool) {
	return requested > 0 && requested <= available
}
GO
cat > stock_test.go <<'GO'
package inventory

import "testing"

func TestReservationBoundary(t *testing.T) {
	if !CanReserve(3, 3) || CanReserve(3, 4) || CanReserve(3, 0) {
		t.Fatal("incorrect reservation decision")
	}
}
GO
mkdir .pdd
cat > .pdd/plan.json <<'JSON'
{
  "schema": 1,
  "name": "Inventory reservation",
  "intent": "Allow exactly positive reservations within available stock",
  "non_goals": ["Correctness of storage, concurrency and callers"],
  "requirements": [
    {"id":"reservation-policy","kind":"formal","text":"Allow exactly quantities within stock","check":"proof","file":"stock.go","function":"CanReserve","ensures":"allowed == (requested > 0 && requested <= available)"},
    {"id":"boundary-cases","kind":"empirical","text":"Exercise zero, exact stock and overstock cases","check":"tests"}
  ],
  "checks": [
    {"id":"proof","kind":"gobra","files":["stock.go"]},
    {"id":"tests","kind":"go-test","package":".","tests":["TestReservationBoundary"]}
  ],
  "trusted_boundary": [],
  "mutations": [
    {"id":"deny-everyone","kind":"last-return","requirement":"reservation-policy","check":"proof","file":"stock.go","find":"","replace":"return false"}
  ]
}
JSON
pdd lint
pdd plan
```

The `ensures` clause captures both directions, so a function that always denies
reservations fails the contract. Review the plan and copy its printed `Digest`
into the next command.

```sh
pdd approve --reviewer YOUR_NAME --expect-digest REVIEWED_DIGEST
pdd prove
pdd status
pdd report --out .pdd/results/inventory.html
```

`reservation-policy` should be `proved` and `boundary-cases` should be
`evidenced`. The HTML links to the negative example and test logs. For your own
project, model caller obligations as explicit trust assumptions with named tests.

## Move the same check into CI

Export the approval after a reviewer has inspected the concrete contract. Commit
it alongside `.pdd/plan.json` and the annotated source.

```sh
pdd export-approval --to .pdd/reviewed-inventory.json
```

A GitHub Actions job can run these steps after checkout and Go setup:

```yaml
- name: Install PDD
  run: go install github.com/cruskell/proofsteer-go/cmd/pdd@v0.4.0
- name: Verify reviewed inventory policy
  run: |
    pdd import-approval --from .pdd/reviewed-inventory.json
    pdd prove
    pdd status
```

The runner needs Go and Docker. When the contract changes, have the reviewer
inspect `pdd plan --diff` and record a new approval. Use an independently
provisioned policy and signed records when CI must check reviewer authority.
[CI integration](ci.md) · [Signed reviews](signed-reviews.md)
