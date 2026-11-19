BEGIN;

CREATE SCHEMA IF NOT EXISTS tickethn_internal;
REVOKE ALL ON SCHEMA tickethn_internal FROM PUBLIC;

CREATE TABLE IF NOT EXISTS tickethn_internal.purchase_emails (
    order_id uuid PRIMARY KEY REFERENCES public.orders(id) ON DELETE CASCADE,
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    attempts integer NOT NULL DEFAULT 0,
    request_body bytea,
    first_attempt_at timestamptz,
    sent_at timestamptz,
    failed_at timestamptz
);

CREATE INDEX IF NOT EXISTS purchase_emails_ready
    ON tickethn_internal.purchase_emails (available_at, order_id)
    WHERE sent_at IS NULL AND failed_at IS NULL;

COMMIT;
