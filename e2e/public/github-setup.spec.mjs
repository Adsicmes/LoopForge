import {test,expect} from '@playwright/test';
import {password,appReady} from './helpers.mjs';

test.use({trace:'off',screenshot:'off'});

test('GitHub authorization stays in one credential row with a separate manual path',async({page},info)=>{
  const errors=[],failed=[];
  page.on('pageerror',error=>errors.push(error.message));
  page.on('response',response=>{if(response.status()>=500)failed.push(response.status()+' '+response.url())});
  await page.goto('/login.html');
  await page.locator('[name="login"]').fill(process.env.LOOPFORGE_TEST_USER||'admin');
  await page.locator('[name="password"]').fill(password());
  await page.locator('#loginSubmit').click();
  await appReady(page);
  await page.waitForLoadState('networkidle');
  const capability=await page.request.get('/api/auth/github').then(response=>response.json());
  await page.goto('/#/settings');
  await expect(page.locator('#personalSettingsContent > .personal-settings-card')).toHaveCount(0);
  // 本用例也跑在 compose.yaml 里 pin 的发布镜像上，那个版本还是一个共享槽位：
  // 有分平台的行就逐个断言，没有就退回旧行名，两种构建都要能过。
  const rowKeys=await page.locator('[data-credential-row]').evaluateAll(els=>els.map(el=>el.dataset.credentialRow));
  if(rowKeys.includes('gitTokenCodeup')){
    await expect(page.locator('[data-credential-row="gitToken"]')).toContainText('GitHub');
    await expect(page.locator('[data-credential-row="gitToken"]')).toContainText('仓库授权');
    await expect(page.locator('[data-credential-row="gitTokenCodeup"]')).toContainText('云效');
    await expect(page.locator('[data-credential-row="gitTokenGongfeng"]')).toContainText('工蜂');
  }else{
    // The release keeps a single shared row under its own wording; only its
    // presence is stable across builds.
    await expect(page.locator('[data-credential-row="gitToken"]')).toHaveCount(1);
  }
  await page.screenshot({path:info.outputPath('github-settings.png'),fullPage:true});
  await page.locator('[data-credential-row="gitToken"] [data-credential-act="setup"]').click();
  const wizard=page.locator('#credentialWizard');
  if(capability.enabled){
    await expect(wizard.locator('[data-github-start]')).toBeVisible();
    await expect(wizard.locator('[data-github-start]')).toBeEnabled();
  }else{
    await expect(wizard.locator('[data-github-start]')).toBeHidden();
    await expect(wizard).toContainText('已停用');
  }
  await expect(wizard.locator('input[name="secret"]')).toHaveCount(0);
  await expect(wizard.locator('.github-admin,[data-github-client]')).toHaveCount(0);
  await expect(wizard).not.toContainText('Client ID');
  await page.screenshot({path:info.outputPath('github-authorization.png'),fullPage:true});
  await wizard.getByRole('tab',{name:'访问令牌'}).click();
  await expect(wizard.locator('input[name="secret"]')).toBeVisible();
  await expect(wizard.locator('[data-github-connect]')).toHaveCount(0);
  await page.screenshot({path:info.outputPath('github-manual.png'),fullPage:true});
  await page.setViewportSize({width:390,height:844});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  await expect(wizard.locator('.credential-dialog')).toBeInViewport();
  await page.screenshot({path:info.outputPath('github-mobile.png'),fullPage:true});
  expect(errors).toEqual([]);
  expect(failed).toEqual([]);
});
