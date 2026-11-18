const { test, expect } = require('@playwright/test');

test('rejects an overdraft without debiting the wallet', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel('Amount').fill('101');
  await page.getByRole('button', { name: 'Withdraw' }).click();
  await expect(page.getByRole('status')).toHaveText('Withdrawal denied.');

  // Withdrawing the entire starting balance proves the rejected attempt did
  // not debit it on this path. The success message uses the server's balance.
  await page.getByLabel('Amount').fill('100');
  await page.getByRole('button', { name: 'Withdraw' }).click();
  await expect(page.getByRole('status')).toHaveText('Remaining balance: 0');
});
