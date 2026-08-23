export type EmailLoginRequest = {
  email: string;
  password: string;
};

export type EmailRegistrationRequest = {
  name: string;
  email: string;
};

export type EmailPasswordResetRequest = {
  email: string;
};

export function normalizeAuthEmail(value: string): string {
  return value.trim().toLowerCase();
}
