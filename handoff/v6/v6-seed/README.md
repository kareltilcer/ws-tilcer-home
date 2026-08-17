# home v6 — the `fin` data migration

Everything needed to move `fin.tilcer.cz`'s months into home's `finance` module and to prove they arrived intact. Governed by `../PRD.md` **§V6-12** (decisions **D91**, **D97**) and `../HANDOFF-8-finance.md` **§7 / §13**.

| File | What it is |
|---|---|
| `fin-months-export.json` | **Provenance.** The raw `GET /months` response from live `fin`, taken **2026-08-17**, unedited. 15 months, `2025-06` … `2026-08`. Keep this forever; everything else is derived from it. |
| `gen_seed.py` | Turns the export into the seed migration, refusing to emit anything that fails its guard rails. |
| `09900_finance_seed.sql` | **The generated migration.** Goes into the repo at `backend/internal/modules/finance/seed/migrations/`. |
| `verify_migration.py` | **The retirement gate (D97).** Compares fin's months against home's, split values included. |

## Status — steps 1 and 2 of §V6-12 are DONE

- **Export taken** 2026-08-17 from the live service: **15 rows**, `2025-06` through `2026-08`, no gaps in the span. All fifteen use the same rates, `20/60/10/10`.
- **Seed generated and checked.** Every row's split was **re-derived with the locked formula and compared to what the live service returned — all 15 matched exactly.** That is the D97 comparison, run a full build early: the formula in `gen_seed.py` is a transcription of `fin`'s `split.go`, and it reproduces the production numbers to the koruna. If the Go port later disagrees with these rows, the port is wrong.
- **Applied to a real SQLite database** carrying the `09001_finance.sql` schema: 15 rows inserted, every `CHECK` and the unique index satisfied; re-applying the file left the count at 15, so the `INSERT OR IGNORE` idempotency holds.
- **The gate script was self-tested** against a correct home-shaped export (exit 0) and against one with a single split value off by 1 (exit 1, naming the month and the field).

Remaining: steps 3–6 — deploy home v6, re-export from both services, run `verify_migration.py`, then retire.

## Regenerating

```bash
python3 gen_seed.py fin-months-export.json > 09900_finance_seed.sql
```

Guard rails, all of which **fail the generation** rather than the migration: rates sum to exactly 100 and are non-negative · incomes are non-negative integers · `month` matches `^\d{4}-(0[1-9]|1[0-2])$` and is unique across the file · every row has an id · **every row carries a `split`, and that split matches the re-derived one**. The table's own rate-sum `CHECK` is the second net.

Output notes: rows are sorted ascending by month; `id`, `created_at` and `updated_at` are preserved **verbatim**, including fin's nanosecond RFC3339 precision (home writes milliseconds — provenance beats cosmetic consistency, and `finance` orders by `month`, never by timestamp); `created_by` is `NULL` because fin recorded no actor.

## Running the gate

After home v6 is live and **while `fin` is still running**:

```bash
# fin (bearer; /months is behind RequireBearer and is unpaged)
curl -s -X POST https://fin.tilcer.cz/login -H 'Content-Type: application/json' \
     -d '{"email":"…","password":"…","site":"fin"}' | jq -r .access_token > /tmp/tok
curl -s https://fin.tilcer.cz/months -H "Authorization: Bearer $(cat /tmp/tok)" > fin-live.json

# home (session cookie; the script accepts the page object or a bare array)
curl -s 'https://home.tilcer.cz/api/finance/months?limit=200' -b "session=…" > home-live.json

python3 verify_migration.py fin-live.json home-live.json
```

Exit 0 opens the gate. **Anything else stops the retirement** — and with no redirect behind the cutover (D96), this script is the only safety net there is.

## Where the seed goes in the repo

`09900_finance_seed.sql` belongs to the **separate, production-only** `finance/seed` migration source (**D91**), *not* the schema source:

```
backend/internal/modules/finance/
    migrations/09001_finance.sql          # schema — bootstrap.MigrationSources(), migrated by tests too
    seed/
        embed.go                          # //go:embed migrations/*.sql
        migrations/09900_finance_seed.sql # THIS FILE — MigrationSourcesWithSeed() only
```

`bootstrap.MigrationFS()` stays schema-only and is what `testsupport.NewDB` uses; `MigrationFSWithSeed()` is the opt-in that `cmd/home` calls. Get this wrong and every module test runs against fifteen months of real household finances.
