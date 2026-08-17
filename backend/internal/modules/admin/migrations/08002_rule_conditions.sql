-- admin module — conditions on rules and summaries, active hours on rules.
--
-- conditions is a JSON block: {"mode": "all"|"any", "items": [{"key","op","value"}]}
-- where key names a METRIC or LIST from the catalogs and a list's value is its
-- length. NULL means "no conditions" — the rule/summary always sends. Validation
-- happens in Go at save time (unknown keys, bad ops, scope rules), same as the
-- template tokens; SQLite only stores the blob.
--
-- active_from_local / active_to_local are "HH:MM" wall-clock bounds in
-- HOME_TIMEZONE. Both set ⇒ the rule only sends inside the window (which may
-- wrap midnight: 22:00–06:00); both NULL ⇒ always. One without the other is
-- refused at save time.

-- +goose Up

-- +goose StatementBegin
ALTER TABLE notification_rules ADD COLUMN conditions TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notification_rules ADD COLUMN active_from_local TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notification_rules ADD COLUMN active_to_local TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notification_schedules ADD COLUMN conditions TEXT;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
ALTER TABLE notification_rules DROP COLUMN conditions;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notification_rules DROP COLUMN active_from_local;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notification_rules DROP COLUMN active_to_local;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE notification_schedules DROP COLUMN conditions;
-- +goose StatementEnd
