export function dateInTimezone(timezone: string, now = new Date()): string {
  const parts = new Intl.DateTimeFormat("en-CA", { timeZone: timezone, year: "numeric", month: "2-digit", day: "2-digit" }).formatToParts(now);
  const part = (type: string) => parts.find((value) => value.type === type)?.value;
  return `${part("year")}-${part("month")}-${part("day")}`;
}

export function addCalendarDays(date: string, days: number): string {
  const [year, month, day] = date.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, day + days)).toISOString().slice(0, 10);
}

export function displaySlot(value: string, timezone: string): string {
  return new Intl.DateTimeFormat("tr-TR", { weekday: "short", hour: "2-digit", minute: "2-digit", timeZone: timezone }).format(new Date(value));
}

export function displayDate(value: string, timezone: string): string {
  const [year, month, day] = value.split("-").map(Number);
  return new Intl.DateTimeFormat("tr-TR", { weekday: "short", month: "short", day: "numeric", timeZone: timezone }).format(new Date(Date.UTC(year, month - 1, day, 12)));
}
