"use client";
import Link from "next/link";
import { FormEvent, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import { EmailPasswordResetRequest, normalizeAuthEmail } from "../../../shared/auth-contract";

export default function ForgotPassword(){const[email,setEmail]=useState("");const[done,setDone]=useState(false);const[busy,setBusy]=useState(false);const[error,setError]=useState("");async function submit(e:FormEvent){e.preventDefault();if(busy)return;setBusy(true);setError("");try{const request:EmailPasswordResetRequest={email:normalizeAuthEmail(email)};await apiClient.post("/v1/public/customer/auth/forgot-password",request);setDone(true)}catch(cause){setError(cause instanceof ApiError?cause.message:"Yeni şifre istenemedi.")}finally{setBusy(false)}}return <main className="account-page"><Link className="eyebrow" href="/login">← Girişe dön</Link><h1>Şifreyi sıfırla</h1>{done?<p className="account-lede" role="status">Bu e-posta adresine ait bir hesap varsa geçici şifre e-posta ile gönderilecek.</p>:<form className="account-form" onSubmit={submit}><label>E-posta<input value={email} onChange={e=>setEmail(e.target.value)} required type="email" autoComplete="email" /></label>{error&&<p className="error" role="alert">{error}</p>}<button className="book-button" disabled={busy}>{busy?"Gönderiliyor…":"Yeni şifre gönder"}</button></form>}</main>}
