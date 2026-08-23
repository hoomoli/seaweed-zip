import React,{useEffect,useState} from 'react';
import {createRoot} from 'react-dom/client';
import {Button,ConfigProvider,Drawer,Empty,Select,Table,Tag,Timeline,Alert} from 'antd';
import type {ColumnsType} from 'antd/es/table';
import {api,artifactRetention,bytes,historyExpiry,pollInterval,progress,type Pack,type Detail,type State} from './model';
import './style.css';
import {en} from './locales/en';
import {zhCN} from './locales/zh-CN';
const date=(n?:number)=>n?new Date(n*1000).toLocaleString('en-GB',{timeZone:'Asia/Shanghai',hour12:false}):'—';
function App(){
 const [locale,setLocale]=useState('en');const t=locale==='zh-CN'?zhCN:en;const labels=t.states;
 useEffect(()=>{document.documentElement.lang=locale},[locale]);
 const [items,setItems]=useState<Pack[]>([]),[page,setPage]=useState(0),[state,setState]=useState(''),[more,setMore]=useState(false),[error,setError]=useState(''),[detail,setDetail]=useState<Detail>(),[selected,setSelected]=useState<string>(),[loading,setLoading]=useState(false),[now,setNow]=useState(Date.now()/1000);
 async function load(){try {const result=await api<{items:Pack[];hasMore:boolean}>(`/api/v1/admin/packs?offset=${page*25}&limit=25&state=${state}`);setItems(result.items);setMore(result.hasMore);setError('');if(selected)setDetail(await api<Detail>(`/api/v1/admin/packs/${selected}`))}catch(e){setError(String(e))}}
 useEffect(()=>{let stopped=false;let timer:ReturnType<typeof setTimeout>;const run=async()=>{setLoading(true);await load();if(stopped)return;setLoading(false);timer=setTimeout(run,pollInterval(items))};void run();return()=>{stopped=true;clearTimeout(timer)}},[page,state,selected,items.some(x=>['PACKING','QUEUED','DELETING'].includes(x.state))]);
 useEffect(()=>{const timer=setInterval(()=>setNow(Date.now()/1000),30000);return()=>clearInterval(timer)},[]);
 async function open(id:string){setSelected(id);try{setDetail(await api<Detail>(`/api/v1/admin/packs/${id}`))}catch(e){setError(String(e))}}
 const columns:ColumnsType<Pack>=[
 {title:t.release,key:'release',render:(_,p)=><div><strong>{p.identity.game}</strong><div className="sub">{p.identity.platform} · {p.identity.release}</div></div>},
 {title:t.id,dataIndex:'packId',render:(id:string)=><button className="id-link" onClick={()=>void open(id)} title={id}>{id.slice(0,12)}</button>},
 {title:t.node,render:(_,p)=>p.identity.node||'—'},
 {title:t.phase,dataIndex:'state',render:(s:State)=><Tag color={s==='READY'?'green':s==='FAILED'?'red':s==='PACKING'?'cyan':'default'}>{labels[s]}</Tag>},
 {title:t.progress,render:(_,p)=><span className="sub">{progress(p,t)}</span>},
 {title:t.ack,render:(_,p)=><span className="sub">{p.ackCount||0} / {p.consumerCount||0}{p.activeDownloads?` · ${t.downloading}`:''}</span>},
 {title:t.size,dataIndex:'bytes',render:(n:number)=><span title={`${n} bytes`}>{n?bytes(n):'—'}</span>},
 {title:t.retention,render:(_,p)=><span title={date(p.deleteAfter)}>{artifactRetention(p,now,t)}</span>},
 {title:t.history,render:(_,p)=><span className="sub">{historyExpiry(new Date(p.createdAt*1000)).toLocaleDateString(locale,{timeZone:'Asia/Shanghai'})}</span>},
 ];
 return <ConfigProvider theme={{token:{colorPrimary:'#17665b',borderRadius:5,fontSize:13,fontFamily:'"Avenir Next", "PingFang SC", sans-serif'},components:{Table:{cellPaddingBlockSM:11,headerBg:'#edf1ec'}}}}>
 <main><header><div className="brand"><span className="mark">≋</span><span>SEAWEED <b>ZIP</b></span></div><div className="boundary">{t.boundary}</div><Select aria-label="Language" value={locale} onChange={setLocale} options={[{value:'en',label:'English'},{value:'zh-CN',label:'zh-CN'}]} /></header>
 <section className="intro"><div><div className="eyebrow">RESOURCE TRANSFER / 01</div><h1>{t.headline}</h1><p>{t.intro}</p></div><div className="retention-note"><b>24h<span>+</span></b><span>{t.minimum}</span><small>04:10 daily · Asia/Shanghai</small></div></section>
 <section className="workspace"><div className="toolbar"><h2>{t.tasks} <span>{items.length}</span></h2><div className="controls"><Select aria-label={t.filter} value={state} onChange={v=>{setState(v);setPage(0)}} options={[{value:'',label:t.all},...Object.entries(labels).map(([value,label])=>({value,label}))]} style={{width:160}}/><Button onClick={()=>void load()} loading={loading}>{t.refresh}</Button></div></div>
 {error&&<Alert type="error" title={t.readFailed} description={error} showIcon/>}
 <div className="desktop-table"><Table rowKey="packId" size="small" dataSource={items} columns={columns} pagination={false} locale={{emptyText:<Empty description={t.empty}/>}}/></div>
 <div className="mobile-list">{!items.length?<Empty description={t.empty}/>:items.map(p=><button key={p.packId} className="pack-card" onClick={()=>void open(p.packId)}><div><strong>{p.identity.game} / {p.identity.release}</strong><Tag>{labels[p.state]}</Tag></div><code>{p.packId.slice(0,12)}</code><p>{progress(p,t)}</p><footer><span>ZIP {p.bytes?bytes(p.bytes):'—'}</span><span>{artifactRetention(p,now,t)}</span></footer></button>)}</div>
 <div className="pagination"><span>{t.page} {page+1} · {t.perPage}</span><Button disabled={!page} onClick={()=>setPage(page-1)}>{t.previous}</Button><Button disabled={!more} onClick={()=>setPage(page+1)}>{t.next}</Button></div></section>
 <footer className="footnote">Store ZIP / BagIt 1.0 <span>{t.notAck}</span></footer>
 <Drawer title={t.evidence} open={!!selected} onClose={()=>{setSelected(undefined);setDetail(undefined)}} size="min(640px, 100vw)">{detail&&<><Tag>{labels[detail.pack.state]}</Tag><h3>{detail.pack.identity.game} · {detail.pack.identity.release}</h3><code className="full-id">{detail.pack.packId}</code><dl><dt>{t.generation}</dt><dd>{detail.pack.generation}</dd><dt>{t.progress}</dt><dd>{progress(detail.pack,t)}</dd><dt>{t.expires}</dt><dd>{date(detail.pack.deleteAfter)} · {artifactRetention(detail.pack,now,t)}</dd><dt>{t.history}</dt><dd>{historyExpiry(new Date(detail.pack.createdAt*1000)).toLocaleString('en-GB',{timeZone:'Asia/Shanghai'})}</dd><dt>ZIP SHA256</dt><dd className="full-id">{detail.pack.artifactSha256||'—'}</dd></dl>{detail.pack.error&&<Alert type="error" title={detail.pack.error}/>}
 <h3>{t.consumers}</h3>{detail.consumers.map(c=><div className="consumer" key={c.consumerId}><div><strong>{c.node||t.unmarked}</strong><p className="sub">{c.deployment||t.noDeployment} · generation {c.generation}</p><Tag>{c.cancelled?t.states.CANCELLED:c.ack?t.verified:(c.downloadLease||0)>now?t.downloadPending:t.notConfirmed}</Tag></div>{!c.cancelled&&!c.ack&&<Button danger size="small" onClick={async()=>{if(!window.confirm(t.cancelConfirm))return;try{await api(`/api/v1/admin/consumers/${c.consumerId}/cancel`,{method:'POST'});await load()}catch(e){setError(String(e))}}}>{t.cancel}</Button>}</div>)}
 <h3>{t.events}</h3><Timeline items={detail.events.map((e,i)=>({content:<div><strong>{e.phase} · {e.code}</strong><p className="sub">{date(e.at)}{i>0?` · ${t.since} ${e.at-detail.events[i-1].at}s`:''}</p></div>}))}/>
 <p className="sub">{t.retry}</p></>}</Drawer>
 </main></ConfigProvider>
}
createRoot(document.getElementById('root')!).render(<React.StrictMode><App/></React.StrictMode>);
