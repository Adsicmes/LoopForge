import test from 'node:test';
import assert from 'node:assert/strict';
import { loadApp } from '../lib/load-app.mjs';

test('Codex subscription setup follows the platform switch and personal status', async (t) => {
  const app = await loadApp({ routes: [
    { method: 'GET', path: '/api/users/1/codex-subscription', body: { enabled: true, authJsonAllowed: true, configured: false } },
  ] });
  t.after(app.close);
  app.evaluate(`state.currentUser={...state.data.users[0],id:1,isGuest:false};state.data.users=[state.currentUser];state.data.capabilities={...(state.data.capabilities||{}),codexSubscription:true}`);

  const direct = app.evaluate(`(() => {
    state.codexSubscription={enabled:true,authJsonAllowed:true};
    const on=codexSubscriptionSetupHTML();
    state.codexSubscription={enabled:false,authJsonAllowed:false};
    const off=codexSubscriptionSetupHTML();
    state.codexSubscription=undefined;
    const bootstrap=codexSubscriptionSetupHTML();
    return {on,off,bootstrap,json:codexAuthJsonAllowed()};
  })()`);
  assert.match(direct.on, /data-codex-sub-act="connect"/);
  assert.doesNotMatch(direct.on, /尚未开启/);
  assert.match(direct.on, /粘贴 auth\.json/);
  assert.match(direct.off, /尚未开启/);
  assert.doesNotMatch(direct.bootstrap, /尚未开启/);
  assert.equal(direct.json, false);

  // Opening personal settings must refresh the personal status, not trust a
  // bootstrap value captured before the administrator enabled the feature.
  await app.evaluate('renderPersonalSettings()');
  const status = app.evaluate('state.codexSubscription');
  assert.equal(status.enabled, true);
  assert.equal(status.authJsonAllowed, true);
  assert.doesNotMatch(app.evaluate('codexSubscriptionSetupHTML()'), /尚未开启/);
});
