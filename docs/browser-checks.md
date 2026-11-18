# Browser checks with Playwright

A `playwright` check runs an existing Playwright Test project in the same kind of
isolated source copy as a Go test. PDD verifies the required test identities in
the final JSON report and saves it with the receipt. HTML reports link to the
execution log, the Playwright JSON, and any saved failure screenshots and traces.

## Run the wallet example

Use a Go toolchain, the pinned Gobra image, and Node.js 20 or newer for this
example. Install Playwright and Chromium once on the machine running the check.
Run these commands from the repository root with a `pdd` binary built from this
checkout.

```sh
browser_tools=$(mktemp -d)
npm install --prefix "$browser_tools" @playwright/test@1.65.1
export PDD_PLAYWRIGHT_CLI="$browser_tools/node_modules/@playwright/test/cli.js"
node "$PDD_PLAYWRIGHT_CLI" install chromium

pdd --plan httpwallet-browser init --from examples/httpwallet/browser/plan.json
pdd --plan httpwallet-browser plan
pdd --plan httpwallet-browser import-approval --from .pdd/reviews/httpwallet-browser.json
pdd --plan httpwallet-browser prove
pdd --plan httpwallet-browser status
pdd --plan httpwallet-browser report --out .pdd/state/httpwallet-browser/report.html
```

Open the generated HTML alongside its `receipts/` directory so relative links
work. The saved example approval is a demonstration assertion. Review and approve
your own project's requirements and trust assumptions separately. On Linux,
`node "$PDD_PLAYWRIGHT_CLI" install --with-deps chromium` can also install the
browser's system dependencies.

## Adopt an existing suite

- Point `PDD_PLAYWRIGHT_CLI` to an absolute `node_modules/@playwright/test/cli.js`
  path. `node` must be on `PATH`. Provision the runner and any CommonJS test
  dependencies together. PDD sets `NODE_PATH` to that `node_modules` directory.
  The check performs no dependency installation.
- Add a check of kind `playwright` with `config`, `project`, and `tests` in the
  [plan format](plan-format.md). Use a standalone Playwright project and a
  repository-relative configuration. PDD runs that project without project
  dependencies and verifies every required test against the completed result.
  Use a config that starts its own local app and keeps output out of the source
  snapshot. The wallet config sets `reuseExistingServer: false`.
- Keep project code, config, and tests in the captured snapshot. You can put the
  preinstalled dependencies outside the repository, or explicitly exclude
  `node_modules` via the reviewed `inputs.exclude` policy. Browser plans allow
  only `node_modules` exclusions in addition to PDD's default reserved paths.
  Use CommonJS-compatible dependency resolution for externally provisioned
  packages. Fully native ESM imports do not use `NODE_PATH`.
- Set `use.screenshot` to `only-on-failure` if you want failure images. PDD
  requests tracing on failure and retains bounded PNG/trace artifacts from
  Playwright's output directory. Open a saved trace with
  `node "$PDD_PLAYWRIGHT_CLI" show-trace PATH_TO_TRACE.zip`. Browser artifacts can
  contain private page content or network data. Share them with the same care as
  test logs.

The runtime and browser are part of the empirical test environment. Playwright's
configuration and tests run with the invoking user's permissions, as Go tests do.
Browser checks provide evidence for the cases that actually ran. Gobra continues
to prove the Go contracts separately.
