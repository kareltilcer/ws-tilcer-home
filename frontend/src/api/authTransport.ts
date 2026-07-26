// Mode B auth constants. Home hosts its own login and owns its session (no JWT in
// the browser), so there is no token transport here — only the outbound links to
// the auth-hosted flows home does NOT implement (password reset, MFA fallback).

// The shared auth service base URL (for the "Zapomněli jste heslo?" link and the
// MFA-required fallback). Same value the backend uses as AUTH_BASE_URL.
export const AUTH_BASE_URL = (import.meta.env.VITE_AUTH_BASE_URL as string | undefined) ?? 'https://auth.tilcer.cz'
