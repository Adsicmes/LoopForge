import test from 'node:test';
import assert from 'node:assert/strict';
import { loadApp, settle, chooseOption } from '../lib/load-app.mjs';

const ORG = '60de7a6852743a5162b5f957';

test('Codeup addresses are recognized as their own provider', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  const parse = (raw) => app.evaluate(`parseGongfengRepoURL(${JSON.stringify(raw)})`);
  const https = parse(`https://codeup.aliyun.com/${ORG}/group/demo.git`);
  assert.equal(https.provider, 'codeup');
  assert.equal(https.project, `codeup:${ORG}/group/demo`);
  assert.equal(https.name, 'demo');
  assert.equal(https.cloneUrl, `https://codeup.aliyun.com/${ORG}/group/demo.git`);

  // Web subpages, the codeup: shorthand and the SSH form all collapse to the same identity.
  assert.equal(parse(`https://codeup.aliyun.com/${ORG}/group/demo/tree/master/src`).project, `codeup:${ORG}/group/demo`);
  assert.equal(parse(`codeup:${ORG}/group/demo`).project, `codeup:${ORG}/group/demo`);
  assert.equal(parse(`git clone git@codeup.aliyun.com:${ORG}/group/demo.git`).project, `codeup:${ORG}/group/demo`);
  // A newer repository address must not be mistaken for a Codeup one.
  assert.equal(parse('https://github.com/alice/demo').provider, 'github');
  assert.equal(parse('https://codeup.example.com/nothex/group/demo'), null);
});

test('the Codeup picker lists organization repositories and fills the form', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  app.router.get(`/api/codeup/repositories`, {
    repositories: [
      { project: `codeup:${ORG}/group/demo`, name: 'demo', webUrl: `https://codeup.aliyun.com/${ORG}/group/demo`, cloneUrl: `https://codeup.aliyun.com/${ORG}/group/demo.git`, defaultBranch: 'master' },
      { project: `codeup:${ORG}/group/api`, name: 'api', webUrl: `https://codeup.aliyun.com/${ORG}/group/api`, cloneUrl: `https://codeup.aliyun.com/${ORG}/group/api.git`, defaultBranch: 'develop' },
    ],
    organization: ORG, total: 2, page: 1, hasMore: false,
  });
  app.evaluate(`
    state.currentUser={id:1,loginName:'owner',gitTokenConfigured:true};
    const address=document.createElement('input');address.id='codeup-address';
    address.value='https://codeup.aliyun.com/${ORG}/group/demo.git';document.body.append(address);
    const host=document.createElement('div');host.id='codeup-test';document.body.append(host);
    window.codeupPicked=null;
    mountCodeupRepoPicker(host,repo=>{window.codeupPicked=repo},{getAddress:()=>address.value});
  `);
  const host = app.document.querySelector('#codeup-test');
  // The organization is read from the address that is already typed.
  assert.equal(host.querySelector('[data-codeup-org]').value, ORG);
  host.querySelector('[data-codeup-load]').click();
  await settle(8);
  const call = app.router.calls.find(c => c.path === '/api/codeup/repositories');
  assert.equal(call.search, `?org=${ORG}`);
  const select = host.querySelector('[data-codeup-repo]');
  assert.equal(host.querySelector('[data-codeup-select-field]').hidden, false);
  assert.equal(select.options.length, 3);
  chooseOption(select, `codeup:${ORG}/group/api`);
  assert.equal(app.evaluate('window.codeupPicked.name'), 'api');
  assert.equal(app.evaluate('window.codeupPicked.defaultBranch'), 'develop');
});

test('the Codeup picker asks for an organization instead of calling the API blindly', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  app.evaluate(`
    state.currentUser={id:1,loginName:'owner',gitTokenConfigured:true};
    const host=document.createElement('div');host.id='codeup-noorq';document.body.append(host);
    mountCodeupRepoPicker(host,()=>{});
  `);
  const host = app.document.querySelector('#codeup-noorq');
  host.querySelector('[data-codeup-load]').click();
  await settle(4);
  assert.equal(app.router.calls.filter(c => c.path === '/api/codeup/repositories').length, 0);
  const error = host.querySelector('[data-codeup-error]');
  assert.equal(error.hidden, false);
  assert.match(error.textContent, /组织 ID/);
});

test('a failed listing surfaces the server message', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  app.router.get('/api/codeup/repositories', { status: 401, body: { error: '云效拒绝了这把个人访问令牌' } });
  app.evaluate(`
    state.currentUser={id:1,loginName:'owner',gitTokenConfigured:true};
    const host=document.createElement('div');host.id='codeup-bad';document.body.append(host);
    mountCodeupRepoPicker(host,()=>{});
  `);
  const host = app.document.querySelector('#codeup-bad');
  host.querySelector('[data-codeup-org]').value = ORG;
  host.querySelector('[data-codeup-load]').click();
  await settle(8);
  const error = host.querySelector('[data-codeup-error]');
  assert.equal(error.hidden, false);
  assert.match(error.textContent, /个人访问令牌/);
});

test('the repo drawer mounts the Codeup picker next to the GitHub one', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  app.evaluate(`
    state.currentUser={id:1,loginName:'owner',gitTokenConfigured:true};
    state.projectAdminDetailId='project-1';
    const drawer=document.querySelector('#repoDrawer');
    openRepoDrawer();
  `);
  await settle(4);
  assert.ok(document_has(app, '#repoDrawer [data-codeup-repositories] [data-codeup-org]'));
});

function document_has(app, selector) {
  return !!app.document.querySelector(selector);
}

test('the repository drawer manages the Codeup webhook secret', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  app.router.post(/^\/api\/repositories\/[^/]+\/webhook-secret$/, { secret: 'fixture-secret-not-real', header: 'X-Codeup-Token' });
  app.router.delete(/^\/api\/repositories\/[^/]+\/webhook-secret$/, { status: 'cleared' });
  app.evaluate(`
    state.currentUser={id:1,loginName:'owner',gitTokenConfigured:true};
    state.data.repositories=state.data.repositories||[];
    state.data.repositories.push({id:'repo-codeup-1',projectId:'project-1',provider:'codeup',name:'demo',
      gongfengProjectId:'codeup:${ORG}/group/demo',webUrl:'https://codeup.aliyun.com/${ORG}/group/demo',defaultBranch:'master'});
    state.projectAdminDetailId='project-1';
    openRepoDrawer('repo-codeup-1');
  `);
  await settle(4);
  const box = app.document.querySelector('#repoDrawerWebhook');
  assert.equal(box.hidden, false);
  const endpoint = box.querySelector('input[readonly]');
  assert.match(endpoint.value, new RegExp(`/api/webhooks/codeup/repo-codeup-1$`));
  assert.match(box.textContent, /还没有配置密钥/);

  box.querySelector('[data-webhook-rotate]').click();
  await settle(8);
  const call = app.router.calls.find(c => c.method === 'POST' && c.path === '/api/repositories/repo-codeup-1/webhook-secret');
  assert.ok(call, 'the rotate call should be sent');
  const secretBox = app.document.querySelector('#repoDrawerWebhook .repo-webhook-secret code');
  assert.equal(secretBox.textContent, 'fixture-secret-not-real');
  assert.match(app.document.querySelector('#repoDrawerWebhook').textContent, /只显示这一次/);
});

test('a GitHub repository shows no Codeup webhook panel', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  app.evaluate(`
    state.currentUser={id:1,loginName:'owner',gitTokenConfigured:true};
    state.data.repositories=state.data.repositories||[];
    state.data.repositories.push({id:'repo-gh-1',projectId:'project-1',provider:'github',name:'gh',
      gongfengProjectId:'github:alice/gh',webUrl:'https://github.com/alice/gh',defaultBranch:'main'});
    state.projectAdminDetailId='project-1';
    openRepoDrawer('repo-gh-1');
  `);
  await settle(4);
  assert.equal(app.document.querySelector('#repoDrawerWebhook').hidden, true);
});
