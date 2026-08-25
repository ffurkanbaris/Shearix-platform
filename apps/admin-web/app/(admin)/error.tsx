"use client";
export default function Error({ reset }: { error: Error; reset: () => void }) { return <main className="centered-state"><h1>Bu sayfa yüklenemedi</h1><p>Lütfen tekrar deneyin.</p><button className="button primary" onClick={reset}>Tekrar dene</button></main>; }
