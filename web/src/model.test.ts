import { describe,it,expect } from 'vitest';
import { bytes,artifactRetention,historyExpiry,progress,pollInterval } from './model';
describe('operational status',()=>{
 it('separates pending cleanup from successful deletion',()=>{expect(artifactRetention({state:'READY',deleteAfter:10},20)).toBe('Pending cleanup');expect(artifactRetention({state:'DELETED',deleteAfter:10},20)).toBe('Cleaned')});
 it('shows real byte/object progress, not a guessed percentage',()=>{expect(progress({doneObjects:1,doneBytes:1024,identity:{objects:[{},{}]}})).toBe('1 / 2 objects · 1 KiB');expect(bytes(1536)).toBe('1.5 KiB')});
 it('clamps calendar months',()=>{expect(historyExpiry(new Date('2024-01-31T00:00:00Z')).toISOString()).toBe('2024-04-30T00:00:00.000Z')});
 it('slows polling without active tasks',()=>{expect(pollInterval([{state:'READY'}])).toBe(30000);expect(pollInterval([{state:'PACKING'}])).toBe(3000)})
});

it('supports translated retention without duplicating state logic',()=>{expect(artifactRetention({state:'DELETED'},20,{cleaned:'Removed'} as never)).toBe('Removed')});

it('accepts no-content logout and sends session-bound CSRF',async()=>{
 const original=globalThis.fetch;
 const calls:{path:string;init?:RequestInit}[]=[];
 globalThis.fetch=async(input,init)=>{calls.push({path:String(input),init});return String(input)==='/auth/session'?new Response(JSON.stringify({csrfToken:'csrf'}),{status:200}):new Response(null,{status:204})};
 try{const {api}=await import('./model');expect(await api('/auth/logout',{method:'POST'})).toBeUndefined();expect(new Headers(calls[1].init?.headers).get('X-CSRF-Token')).toBe('csrf')}finally{globalThis.fetch=original}
});
