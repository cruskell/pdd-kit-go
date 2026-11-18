const path = require('node:path');

const origin = 'http://127.0.0.1:38479';
module.exports = {
  testDir: __dirname,
  workers: 1,
  projects: [{
    name: 'chromium',
    use: { browserName: 'chromium', baseURL: origin, screenshot: 'only-on-failure' },
  }],
  webServer: {
    command: 'go run ./examples/httpwallet/cmd/wallet -listen 127.0.0.1:38479',
    cwd: path.resolve(__dirname, '../../..'),
    url: origin,
    reuseExistingServer: false,
    timeout: 120_000,
  },
};
