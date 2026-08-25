import Link from "next/link";

export default function NotFound() {
  return (
    <main className="not-found-page">
      <p className="not-found-code">404</p>
      <h1>Sayfa bulunamadı</h1>
      <p>Aradığınız sayfa kaldırılmış, taşınmış veya hiç var olmamış olabilir.</p>
      <Link className="not-found-link" href="/dashboard">
        Yönetim paneline dön
      </Link>
    </main>
  );
}
