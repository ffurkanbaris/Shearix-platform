export function apiErrorMessage(status: number, fallback?: string): string {
  if (fallback) return fallback;
  if (status === 400) return "Lütfen girdiğiniz bilgileri kontrol edin.";
  if (status === 401) return "Oturumunuzun süresi doldu. Lütfen yeniden giriş yapın.";
  if (status === 403) return "Bu işlemi yapmaya yetkiniz yok.";
  if (status === 404) return "İstenen kayıt bulunamadı.";
  if (status === 409) return "Bu işlem mevcut verilerle çakışıyor. Sayfayı yenileyip tekrar deneyin.";
  if (status === 429) return "Çok fazla deneme yaptınız. Lütfen bekleyip tekrar deneyin.";
  return "Bir sorun oluştu. Lütfen tekrar deneyin.";
}

export function currency(amount: string, code: string): string {
  const value = Number(amount);
  if (!Number.isFinite(value)) return `${amount} ${code}`;
  return new Intl.NumberFormat("tr-TR", { style: "currency", currency: code }).format(value);
}

export function displayDateTime(value: string, timezone?: string): string {
  return new Intl.DateTimeFormat("tr-TR", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(new Date(value));
}

export function dateKeyInTimezone(value: string | Date, timezone: string): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    timeZone: timezone,
  }).formatToParts(typeof value === "string" ? new Date(value) : value);
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find((item) => item.type === type)?.value;
  return `${part("year")}-${part("month")}-${part("day")}`;
}

export function localDate(value = new Date()): string {
  const offset = value.getTimezoneOffset();
  return new Date(value.getTime() - offset * 60_000).toISOString().slice(0, 10);
}

export function toLocalInput(value: string): string {
  const date = new Date(value);
  const offset = date.getTimezoneOffset();
  return new Date(date.getTime() - offset * 60_000).toISOString().slice(0, 16);
}
