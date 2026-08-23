-- Migration 000002 intentionally accepted arbitrary text for the original
-- idempotency key and fingerprint. Normalize those legacy values before
-- 000004 adds the bounded key and SHA-256 fingerprint constraints. This file
-- is ordered between immutable migrations 000003 and 000004; databases that
-- already applied 000004 execute it later as a harmless no-op.
UPDATE public.idempotency_keys
SET fingerprint = encode(digest(fingerprint, 'sha256'), 'hex')
WHERE fingerprint !~ '^[0-9a-f]{64}$';

UPDATE public.idempotency_keys
SET key = 'legacy-' || encode(digest(key, 'sha256'), 'hex')
WHERE length(key) NOT BETWEEN 1 AND 128
   OR key !~ '^[A-Za-z0-9._:-]+$';
