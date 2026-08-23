"use client";
export default function Error({ reset }: { error: Error; reset: () => void }) { return <main className="centered-state"><h1>We could not load this page</h1><p>Please try again.</p><button className="button primary" onClick={reset}>Try again</button></main>; }
