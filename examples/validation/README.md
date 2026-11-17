# Permission-aware slice validation

`AllNonnegative` accepts exactly slices whose elements are all nonnegative. The
production Go loop has a quantified postcondition, explicit fractional read
permissions and loop invariants. It preserves elements and read permissions.
The explicit length bound is the largest 64-bit Go `int`, matching this adapter's
64-bit target. Empty and nil slices satisfy the predicate.

```sh
pdd --plan validation init --from examples/validation/plan.json
pdd --plan validation plan
# Record your own review. For authenticated CI, follow docs/signed-reviews.md.
pdd --plan validation approve --reviewer YOUR_NAME
pdd --plan validation prove
```

The pinned verifier proves the real slice implementation with overflow checks.
Two mutants reject zero and skip the final element; each must fail at the exact
quantified postcondition. Runtime tests check nil/empty input, integer extremes,
negative first/last elements and non-modification. Standard/external library
behavior is not silently assumed as formal evidence.
