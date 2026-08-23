import { test as base,expect } from '@playwright/test';
import {zhCN} from '../src/locales/zh-CN';
const test=base.extend<{antdWarnings:string[]}>({antdWarnings:[async({page},use)=>{
 const warnings:string[]=[];
 page.on('console',message=>{if(message.text().includes('[antd:')&&message.text().includes('deprecated'))warnings.push(message.text())});
 await use(warnings);
 expect(warnings,'Upgraded components must use supported Ant Design APIs').toEqual([]);
},{auto:true}]});
const fixture={packId:'a'.repeat(64),state:'READY',generation:1,consumerCount:1,ackCount:0,identity:{game:'Test resources',platform:'android',release:'1.0.0',node:'test-node',objects:[{sha256:'b'.repeat(64),size:1024}]},doneBytes:1024,doneObjects:1,bytes:1536,createdAt:Math.floor(Date.now()/1000)-86400,deleteAfter:Math.floor(Date.now()/1000)-60};
test('desktop evidence keeps drawer width and timeline content after upgrade',async({page})=>{
 await page.route('**/api/**',route=>route.fulfill({json:route.request().url().endsWith('/'+fixture.packId)?{pack:fixture,consumers:[],events:[{phase:'READY',code:'VERIFIED',at:fixture.createdAt+5}]}:{items:[fixture],hasMore:false}}));
 await page.goto('/');
 await page.locator('.id-link').click();
 const dialog=page.getByRole('dialog');
 await expect(dialog).toBeVisible();
 await expect(page.getByText('READY · VERIFIED')).toBeVisible();
 await expect.poll(async()=>Math.round((await dialog.boundingBox())!.width)).toBe(640);
});
test('empty desktop starts without demonstration tasks',async({page})=>{await page.route('**/api/**',route=>route.fulfill({json:{items:[],hasMore:false}}));await page.goto('/');await expect(page.getByText('No pack tasks').first()).toBeVisible();await expect(page.getByRole('heading',{name:'Pack tasks'})).toBeVisible();await page.screenshot({path:'test-results/desktop.png',fullPage:true,animations:'disabled'})});
test('shows pending cleanup, separate history and real progress',async({page})=>{await page.route('**/api/**',route=>route.fulfill({json:{items:[fixture],hasMore:false}}));await page.goto('/');await expect(page.getByText('Pending cleanup').first()).toBeVisible();await expect(page.getByText('1 / 1 objects · 1 KiB').first()).toBeVisible();await expect(page.getByText('1.5 KiB').first()).toBeVisible();await page.screenshot({path:'test-results/desktop-task.png',fullPage:true,animations:'disabled'})});
test('mobile has no horizontal overflow and shows details',async({page})=>{await page.setViewportSize({width:390,height:844});await page.route('**/api/**',route=>{if(route.request().url().endsWith('/'+'a'.repeat(64)))return route.fulfill({json:{pack:fixture,consumers:[{consumerId:'test-consumer',generation:1,ack:false,cancelled:false,node:'test-node',createdAt:fixture.createdAt}],events:[{phase:'READY',code:'VERIFIED',at:fixture.createdAt+5}]}});return route.fulfill({json:{items:[fixture],hasMore:false}})});await page.goto('/');await expect(page.locator('.pack-card')).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBeTruthy();await page.screenshot({path:'test-results/mobile.png',fullPage:true,animations:'disabled'});await page.locator('.pack-card').click();await expect(page.getByText('Task evidence')).toBeVisible();await expect(page.getByText('Not acknowledged')).toBeVisible();await page.screenshot({path:'test-results/mobile-detail.png',fullPage:true,animations:'disabled'})});
test('failed API is actionable, not fake task data',async({page})=>{await page.route('**/api/**',route=>route.fulfill({status:403,json:{phase:'authentication',code:'ADMIN_AUTH_REQUIRED',requestId:'safe-request'}}));await page.goto('/');await expect(page.getByText('Read failed')).toBeVisible();await expect(page.getByText(/ADMIN_AUTH_REQUIRED/)).toBeVisible()});
test('production server rejects anonymous management and malformed source submission',async({request})=>{for(const path of ['/','/api/v1/admin/packs']){const response=await request.get('http://127.0.0.1:8089'+path);expect(response.status()).toBe(403)}const response=await request.post('http://127.0.0.1:8089/api/v1/packs',{data:{}});expect(response.status()).toBe(400)});

test('language switch keeps operational state localized',async({page})=>{await page.route('**/api/**',route=>route.fulfill({json:{items:[fixture],hasMore:false}}));await page.goto('/');await page.getByText('English',{exact:true}).click();await page.locator('.ant-select-item-option-content').filter({hasText:'zh-CN'}).click();await expect(page.locator('html')).toHaveAttribute('lang','zh-CN');await expect(page.getByText(zhCN.pending).first()).toBeVisible();});

test('logout handles 204 and leaves the management screen',async({page})=>{
 await page.route('**/api/**',route=>route.fulfill({json:{items:[fixture],hasMore:false}}));
 await page.route('**/auth/session',route=>route.fulfill({json:{csrfToken:'csrf-test'}}));
 await page.route('**/auth/logout',route=>{expect(route.request().headers()['x-csrf-token']).toBe('csrf-test');return route.fulfill({status:204})});
 await page.route('**/auth/login',route=>route.fulfill({contentType:'text/html',body:'<p>Identity provider login</p>'}));
 await page.goto('/');await page.getByRole('button',{name:'Sign out',exact:true}).click();await expect(page).toHaveURL(/\/auth\/login$/);await expect(page.getByText('Identity provider login')).toBeVisible();
});
