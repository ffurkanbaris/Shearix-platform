"use client";
export default function Error({ reset }: { error: Error; reset: () => void }) { return <main className="page centered"><h1>We could not load this page</h1><p>Please try again.</p><button className="book-button" onClick={reset}>Try again</button></main>; }
