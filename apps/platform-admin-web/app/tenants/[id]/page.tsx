"use client";
import { useEffect, useState } from "react";

export default function TenantDetail({params}:{params:Promise<{id:string}>}) {
  const [id,setId]=useState("");
  const [data,setData]=useState<any>({});
  const [error,setError]=useState("");
  const [notice,setNotice]=useState("");
  const [confirmSuspend,setConfirmSuspend]=useState(false);
  const [pending,setPending]=useState(false);

  const load=async(value:string)=>{try{const paths=[`tenants/${value}`,`tenants/${value}/domains`,`tenants/${value}/owners`,`audit?tenant_id=${value}`,"health"];const [tenant,domains,owners,audit,health]=await Promise.all(paths.map(p=>fetch(`/api/platform/${p}`).then(r=>{if(!r.ok)throw new Error(`${p}: ${r.status}`);return r.json()})));setData({tenant,domains,owners,audit,health});setError("")}catch(reason){setError(reason instanceof Error?reason.message:"Yüklenemedi")}};
  useEffect(()=>{void params.then(p=>{setId(p.id);void load(p.id)})},[params]);

  const change=async(action:"suspend"|"activate")=>{
    setPending(true);setError("");setNotice("");
    try{const response=await fetch(`/api/platform/tenants/${id}/${action}`,{method:"POST"});if(!response.ok)throw new Error(`İşlem başarısız: ${response.status}`);const tenant=await response.json();setData((current:any)=>({...current,tenant}));setConfirmSuspend(false);setNotice(action==="suspend"?"Kiracı askıya alındı. Alan adları artık dış isteklere yanıt vermiyor.":"Kiracı yeniden etkinleştirildi.");await load(id)}catch(reason){setError(reason instanceof Error?reason.message:"İşlem tamamlanamadı")}finally{setPending(false)}};

  if(!data.tenant)return <main className="wrap"><a href="/">← Kiracılar</a><p>{error||"Yükleniyor…"}</p></main>;
  const active=data.tenant.status==="active";
  return <main className="wrap"><a href="/">← Kiracılar</a><div className="top"><div><p className="eyebrow">Kiracı / {active?"Etkin":"Askıda"}</p><h1>{data.tenant.name}</h1><p className="muted">{data.tenant.id}</p></div>{active?<button className="danger" disabled={pending} onClick={()=>setConfirmSuspend(true)}>Askıya al</button>:<button className="primary" disabled={pending} onClick={()=>void change("activate")}>{pending?"Etkinleştiriliyor…":"Yeniden etkinleştir"}</button>}</div>
    {confirmSuspend&&<section className="action-panel" role="alertdialog" aria-labelledby="suspend-title"><div><h2 id="suspend-title">Bu kiracı askıya alınsın mı?</h2><p>Rezervasyon ve yönetim alan adları erişilemez olacak. Veriler silinmez; kiracıyı daha sonra yeniden etkinleştirebilirsiniz.</p></div><div className="action-buttons"><button onClick={()=>setConfirmSuspend(false)} disabled={pending}>Vazgeç</button><button className="danger" onClick={()=>void change("suspend")} disabled={pending}>{pending?"Askıya alınıyor…":"Evet, askıya al"}</button></div></section>}
    {notice&&<p className="notice" role="status">{notice}</p>}{error&&<p className="error-notice" role="alert">{error}</p>}
    <section className="grid"><div className="card"><h2>Alan adları ({data.domains.length})</h2>{data.domains.map((d:any)=><p className="breakable" key={d.id}>{d.hostname} · {d.domain_type} · {d.verified?"doğrulandı":"bekliyor"} · {d.active?"etkin":"pasif"}</p>)}</div><div className="card"><h2>OWNER hesapları ({data.owners.length})</h2>{data.owners.map((o:any)=><p className="breakable" key={o.identity_id}>{o.email}<br/><span className="muted">kimlik {o.identity_status}, üyelik {o.membership_status}, parola değişimi {o.must_change_password?"gerekli":"tamamlandı"}, teslimat {o.delivery_status}</span></p>)}</div><div className="card"><h2>Temel kullanım</h2><p>Alan adı: {data.domains.length}</p><p>OWNER: {data.owners.length}</p></div></section>
    <section className="card"><h2>Servis hazırlığı ve hata göstergeleri</h2>{Object.entries(data.health).map(([name,value]:any)=><div key={name}><span className="badge">{name}: {value.ready?"hazır":"hazır değil"}</span>{value.failure_indicators&&<span className="muted"> {Object.entries(value.failure_indicators).map(([key,count])=>`${key}: ${count}`).join(" · ")||"hata göstergesi 0"}</span>}</div>)}</section>
    <section className="card"><h2>Platform işlem günlüğü</h2>{data.audit.map((a:any)=><p key={a.id}>{new Date(a.created_at).toLocaleString("tr-TR")} · {a.actor} · {a.action}</p>)}{!data.audit.length&&<p>İşlem kaydı yok.</p>}</section></main>;
}
