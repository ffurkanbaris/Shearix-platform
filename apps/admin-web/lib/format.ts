export function apiErrorMessage(status: number, fallback?: string): string {
  if (fallback) return fallback;
  if (status === 400) return "Please check the entered information.";
  if (status === 401) return "Your session has expired. Please sign in again.";
  if (status === 403) return "You do not have permission to perform this action.";
  if (status === 404) return "The requested record was not found.";
  if (status === 409) return "This conflicts with the current data. Refresh and try again.";
  if (status === 429) return "Too many attempts. Please wait and try again.";
  return "Something went wrong. Please try again.";
}

export function currency(amount: string, code: string): string {
  const value = Number(amount);
  if (!Number.isFinite(value)) return `${amount} ${code}`;
  return new Intl.NumberFormat(undefined, { style: "currency", currency: code }).format(value);
}

export function displayDateTime(value: string, timezone?: string): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(new Date(value));
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
