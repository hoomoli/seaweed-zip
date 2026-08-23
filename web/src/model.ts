import {en,type Translation} from './locales/en';
export type State = 'QUEUED'|'VALIDATING'|'PACKING'|'READY'|'DELETING'|'DELETED'|'WAITING_AUTH'|'FAILED'|'CANCELLED';
export interface Pack {packId:string;state:State;generation:number;identity:{game:string;platform:string;release:string;node?:string;objects:{sha256:string;size:number}[]};doneBytes:number;doneObjects:number;bytes:number;createdAt:number;readyAt?:number;deleteAfter?:number;deletedAt?:number;error?:string;artifactSha256?:string;consumerCount?:number;ackCount?:number;activeDownloads?:number}
export interface Consumer {consumerId:string;generation:number;ack:boolean;cancelled:boolean;node?:string;deployment?:string;createdAt:number;downloadLease?:number}
export interface Detail {pack:Pack;consumers:Consumer[];events:{phase:string;code:string;at:number}[]}
export function bytes(n:number):string {const units=['B','KiB','MiB','GiB','TiB'];let i=0;while(n>=1024&&i<units.length-1){n/=1024;i++}return `${Number(n.toFixed(1))} ${units[i]}`}
export function artifactRetention(p:{state:string;deleteAfter?:number},now=Date.now()/1000,t:Translation=en):string {if(p.state==='DELETED')return t.cleaned;if(p.state==='DELETING')return t.cleaning;if(!p.deleteAfter)return t.notCreated;if(p.deleteAfter<=now)return t.pending;const hours=Math.ceil((p.deleteAfter-now)/3600);return `${hours} ${t.hours}`}
export function historyExpiry(d:Date):Date {const result=new Date(d);const day=result.getUTCDate();result.setUTCDate(1);result.setUTCMonth(result.getUTCMonth()+3);const last=new Date(Date.UTC(result.getUTCFullYear(),result.getUTCMonth()+1,0)).getUTCDate();result.setUTCDate(Math.min(day,last));return result}
export function progress(p:{doneObjects:number;doneBytes:number;identity:{objects:unknown[]}},t:Translation=en):string{return `${p.doneObjects} / ${p.identity.objects.length} ${t.objects} · ${bytes(p.doneBytes)}`}
export function pollInterval(p:{state:string}[]):number{return p.some(x=>['QUEUED','VALIDATING','PACKING','DELETING'].includes(x.state))?3000:30000}
export const labels:Record<State,string>=en.states;
export async function api<T>(path:string,init?:RequestInit):Promise<T>{
 const headers=new Headers(init?.headers);
 if(init?.method && !['GET','HEAD'].includes(init.method.toUpperCase())){
  const session=await fetch('/auth/session');
  if(!session.ok)throw new Error('Management login expired');
  headers.set('X-CSRF-Token',(await session.json()).csrfToken);
 }
 const response=await fetch(path,{...init,headers});if(!response.ok){let code=`HTTP ${response.status}`;try {const body=await response.json();code=`${body.phase}: ${body.code} · ${body.requestId}`}catch{/* Ignore non-problem upstream responses. */}throw new Error(code)}if(response.status===204)return undefined as T;return response.json() as Promise<T>
}
