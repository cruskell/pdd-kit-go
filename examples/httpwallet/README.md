# HTTP wallet example

This runnable Go application connects a Gobra-verified production decision to an
HTTP handler. Start it from the repository root:

```sh
go run ./examples/httpwallet/cmd/wallet
curl -i http://127.0.0.1:8080/v1/withdraw \
  -H 'Authorization: Bearer demo-alice' \
  -H 'Content-Type: application/json' -d '{"amount":40}'
```

The response is `200` with `{"balance":60}`. The in-memory balance starts at 100;
a second request for 100 returns `403`. `demo-locked` is always denied. Restarting
resets all balances. These public static credentials, single-process memory and
plain local HTTP are demonstration boundaries, not a deployable payment service.
The listen address defaults to `127.0.0.1:8080`; `-listen` overrides it.

Build the PDD CLI, then initialize the example as a separate plan:

```sh
go build -o bin/pdd ./cmd/pdd
bin/pdd --plan httpwallet init --from examples/httpwallet/plan.json
bin/pdd --plan httpwallet plan
# Read intent, contracts, mutants, limitations and both trust assumptions first.
bin/pdd --plan httpwallet approve --reviewer YOUR_NAME \
  --accept-tcb wallet-sessions,wallet-runtime
bin/pdd --plan httpwallet prove
bin/pdd --plan httpwallet status
```

Gobra checks `policy.CanWithdraw` for all Go integer and boolean inputs, then
rejects three parseable implementations that ignore locks, allow zero amounts
or allow overdrafts. The handler calls this same production function while holding
the balance mutex. It validates a single exact integer `amount`, rejects oversized
bodies and ambiguous credentials, and changes the balance only on approval.

The named empirical tests cover status codes, boundary balances, invalid JSON,
unknown and locked sessions, and forty concurrent TCP requests sharing one
balance. Twenty withdrawals of five succeed and twenty are denied. Run the race
check separately with `go test -race ./examples/httpwallet/...`.

Only the decision has formal evidence. Tests provide finite empirical evidence
for HTTP, session lookup and atomic updates. Client disconnects may occur after a
commit; retries are not idempotent. No durable storage, distributed locking,
identity verification, TLS or settlement guarantees are claimed. The reviewable
plan records these limits and the runtime/session trust boundary explicitly.
