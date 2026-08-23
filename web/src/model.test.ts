import { describe,it,expect } from 'vitest';
import { bytes,artifactRetention,historyExpiry,progress,pollInterval } from './model';
describe('operational status',()=>{
 it('separates pending cleanup from successful deletion',()=>{expect(artifactRetention({state:'READY',deleteAfter:10},20)).toBe('Pending cleanup');expect(artifactRetention({state:'DELETED',deleteAfter:10},20)).toBe('Cleaned')});
 it('shows real byte/object progress, not a guessed percentage',()=>{expect(progress({doneObjects:1,doneBytes:1024,identity:{objects:[{},{}]}})).toBe('1 / 2 objects · 1 KiB');expect(bytes(1536)).toBe('1.5 KiB')});
 it('clamps calendar months',()=>{expect(historyExpiry(new Date('2024-01-31T00:00:00Z')).toISOString()).toBe('2024-04-30T00:00:00.000Z')});
 it('slows polling without active tasks',()=>{expect(pollInterval([{state:'READY'}])).toBe(30000);expect(pollInterval([{state:'PACKING'}])).toBe(3000)})
});

it('supports translated retention without duplicating state logic',()=>{expect(artifactRetention({state:'DELETED'},20,{cleaned:'Removed'} as never)).toBe('Removed')});
