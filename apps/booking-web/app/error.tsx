"use client";
export default function Error({ reset }: { error: Error; reset: () => void }) { return <main className="page centered"><h1>Bu sayfa yüklenemedi</h1><p>Lütfen tekrar deneyin.</p><button className="book-button" onClick={reset}>Tekrar dene</button></main>; }
