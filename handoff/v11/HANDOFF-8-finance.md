# Home — Module 8: `finance` (Finance) + the `fin` migration & retirement

> Build brief for Claude Code. Source of truth for behaviour: `PRD.md` **§V6-1…§V6-12** (decisions **D81–D98**, **FR-F1–F10**). Data shapes: `openapi.yaml` **0.8.0** (tag `finance`). Read `HANDOFF.md` §3 (module registry) and `HANDOFF-1-logging.md` (the audit spine) first. The **reference implementation** is the retiring service's repo, `github.com/kareltilcer/ws-tilcer-fin` — specifically `backend/internal/months/` and `frontend/src/components/FlowViz.tsx`.
>
> **This is the only module so far that replaces a running service.** Building it is half the job; **§13** (migrate the data, verify it, retire `fin`) is the other half, and its steps are gated on each other.

## The model in one paragraph

`finance` records **one row per calendar month**: two incomes and four rates. Everything else — nine money values describing how those incomes flow into five accounts — is **derived on read** by a formula that is **locked** and must be ported byte-for-byte from `fin`. There is no scheduler, no blob store, no new platform strand, no new env var, no external call. It is the simplest module in Home by a wide margin, and the risk is concentrated in two places that have nothing to do with difficulty: **the formula must be identical** (§2), and **the historic data must arrive intact and be proven intact before `fin` is switched off** (§7, §13).

**Every mutation writes an audit event in the same transaction** (`HANDOFF-1`), which is new — `fin` had no audit trail at all. `month.update` carries a **field-level diff**, so a rate correction is answerable after the fact.

## Build order

1. **§1 migration + §2 the split** — port `split.go` and its test FIRST, before any storage or HTTP exists. It is the thing that must not drift, and it is testable in isolation in ten minutes.
2. **§3 store + §4 service** (validation, audit-in-tx, delete) → **§5 endpoints**.
3. **§6 module registration** — widget, metrics, list, audit actions, `bootstrap` wiring.
4. **§7 the seed source** + the `testsupport` split. Do this before the frontend so every later test runs against the real shape.
5. **§8–§9 frontend** — list, flow visualisation, form, widget, nav, copy.
6. **§12 tests**, then **§13** — migrate, verify, and only then retire `fin`.

---

## 1. Data model (PRD §V6-5) — one table, block 09

`internal/modules/finance/migrations/09001_finance.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
-- finance module — Finance tables (PRD §V6-5, HANDOFF-8 §1). Version block 09000:
-- applied last in the one Goose sequence (… documents(07) → admin(08) → finance(09),
-- see bootstrap.MigrationSources). Nothing is seeded HERE — the historic months from
-- the retiring `fin` service arrive through a SEPARATE migration source (§7).
--
-- Column names are carried over verbatim from `fin` (PRD D83). Only the table is
-- namespaced: home is one database holding eight modules' tables, and a bare
-- `months` would be the most collision-prone name in it.
--
-- Inputs only. The nine-value split is DERIVED ON READ (PRD D82) — storing it
-- would create a second source of truth for a formula that is deliberately locked.
CREATE TABLE finance_months (
    id               TEXT PRIMARY KEY,                    -- UUIDv7; seeded rows keep fin's id
    month            TEXT NOT NULL,                       -- YYYY-MM
    income_kaja      INTEGER NOT NULL CHECK (income_kaja >= 0),
    income_andy      INTEGER NOT NULL CHECK (income_andy >= 0),
    rate_personal    INTEGER NOT NULL CHECK (rate_personal >= 0),
    rate_operational INTEGER NOT NULL CHECK (rate_operational >= 0),
    rate_fun         INTEGER NOT NULL CHECK (rate_fun >= 0),
    rate_nofun       INTEGER NOT NULL CHECK (rate_nofun >= 0),
    created_by       TEXT,                                -- NULL for seeded rows (fin recorded no actor)
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    CHECK (rate_personal + rate_operational + rate_fun + rate_nofun = 100)
);
-- +goose StatementEnd

-- Delete is HARD (PRD D87, Karel 2026-08-17) — no deleted_at, so a plain unique
-- index, exactly as fin's 0001_init had it. The month.delete audit event carries a
-- full-row diff, which is what makes an unrecoverable delete acceptable here.
-- +goose StatementBegin
CREATE UNIQUE INDEX ux_finance_months_month ON finance_months (month);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_finance_months_month_desc ON finance_months (month DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS finance_months;
-- +goose StatementEnd
```

**Keep the table-level rate-sum CHECK.** It is redundant with service validation for normal writes, and that is not why it is there: it makes a bad *seed* row fail at migration time, loudly, instead of at read time, quietly.

---

## 2. The split — port it, do not rewrite it (PRD D82, FR-F3)

`internal/modules/finance/split.go` is a **direct port** of `fin`'s `backend/internal/months/split.go`. Copy it, change the package name, change nothing else about the arithmetic:

```go
func Compute(incomeKaja, incomeAndy int, r Rates) Split {
	total := incomeKaja + incomeAndy
	p := float64(r.Personal) / 100
	f := float64(r.Fun) / 100
	n := float64(r.NoFun) / 100

	personalKaja := int(math.Round(float64(incomeKaja) * p))
	personalAndy := int(math.Round(float64(incomeAndy) * p))

	toOperationalKaja := incomeKaja - personalKaja
	toOperationalAndy := incomeAndy - personalAndy
	opReceived := toOperationalKaja + toOperationalAndy

	fun := int(math.Round(float64(total) * f))
	noFun := int(math.Round(float64(total) * n))
	needs := opReceived - fun - noFun
	// … assemble Split
}
```

**The order is the specification.** Personal is rounded first, so `personal + toOperational == income` holds per person. Savings round off the total, not off what the operational account received. `needs` is a **subtraction, never a rounding** — it is where every rounding error is deliberately parked so the totals reconcile. Any "tidy-up" that rounds `needs` directly, or that computes `toOperational` as `round(income × (1−p))`, silently breaks an invariant and will not be caught by the worked example alone.

The **JSON tags change** (snake_case, D92) but the **field names and semantics do not**. Port `split_test.go` alongside, then add the property test in §12.

**One inherited edge case to know about before you "fix" it.** `needs` can come out **negative, by at most 2 CZK**. Provable, not empirical: personal rounding moves `opReceived` by ≤ 1 and each savings rounding by ≤ ½, so `needs ≥ T×o/100 − 2` — meaning `needs < 0` requires **`T × o < 200`**. That happens two ways: at **`operational = 0`** for any income (worst case −2 — `6 887 385 / 2 030 905`, `90/0/5/5`), and at **`operational ≥ 1`** only when total income is under 200 Kč (`(0, 50)` at `1/1/1/97` → −1). It is a rounding remainder, the invariants hold in every case, and **clamping it at zero would break** `fun + noFun + needs == opReceived`. Leave the arithmetic alone, do **not** assert "negative ⇒ operational == 0", render `0 Kč` with a footnote in the UI, and put **both** forms in the test table.

---

## 3. Store — `store.go`

Plain `database/sql`, the shape `todo`/`notes` use. Reads compose the split in Go after scanning inputs; **the split is never in a query**.

- `List(ctx, limit int, cursor string) ([]Month, string, error)` — `ORDER BY month DESC`, keyset on `month` (not on `id`: the user-visible order is by month, and month is unique).
- `Get(ctx, id) (Month, error)` · `Insert(ctx, tx, m)` · `Update(ctx, tx, m)` · `Delete(ctx, tx, id)` — a real `DELETE FROM`.
- `Latest(ctx) (Month, bool, error)` — for the widget.
- `ForMonth(ctx, ym string) (Month, bool, error)` — for the widget and `finance.*_current` metrics.
- `RecordedMonths(ctx) ([]string, error)` — the ordered `YYYY-MM` list backing `missing_months` (metric and list read the same function, which is what makes them agree by construction).

## 4. Service — `service.go`

Same shape as every other module: `WithTx` + audit-in-tx + notify-after-commit.

**Validation** (all `422`, one message each, Czech):

- `month` matches `^\d{4}-(0[1-9]|1[0-2])$`;
- both incomes are integers `>= 0`;
- on create: all four rates present; on update: `rates` present ⇒ **all four present** (a partial rates block is 422, PRD FR-F4);
- `personal + operational + fun + nofun == 100`;
- duplicate `month` ⇒ **409** (map the unique-index violation, and check first so the message is useful).

**Mutations:**

```go
func (s *Service) Create(ctx context.Context, in MonthInput) (Month, error) {
	// validate → appdb.WithTx → store.Insert → s.sink.Record(ctx, tx, audit.Event{
	//     Module:     audit.ModuleFinance,   // add the constant; never a string literal
	//     Action:     "month.create",
	//     EntityType: "finance_month",
	//     EntityID:   id,
	//     Summary:    "Přidán měsíc srpen 2026",   // Czech; a v5 trigger's default body IS this
	//     Changes:    fieldDiff(nil, &m),          // []audit.Change{Field, Old, New *string}
	// }) → commit → notify(ctx, "finance.changed", …)
}
```

*(The write API is `audit.Sink.Record(ctx, *sql.Tx, audit.Event) (eventID, error)` — note **`Event`**, not the notifier's `Entry`. Actor and request context are **not** fields: the sink reads them from the request context so a handler cannot forge them. A `Record` error must propagate so the caller's transaction rolls back — an action that succeeds unlogged is the bug the spine exists to prevent.)*

`month.update` records a **field-level diff** across `month`, `income_kaja`, `income_andy` and the four rates (the hybrid audit rule). `month.delete` records whether it was soft or hard. Nothing else in the module writes.

**Delete is hard (D87):** `DELETE FROM finance_months WHERE id = ?`. No `deleted_at`, no `?hard=true`, no admin tier — an ordinary `RequireWrite` route like create and edit.

**Read the row BEFORE deleting it**, inside the same transaction, and write `month.delete` with a **full-row diff**: `month`, both incomes and all four rates, each as `audit.Change{Field: …, Old: audit.Ptr(oldValue), New: nil}`. This is not decoration — with the row gone, the audit event is the only surviving record of what the month held, and it is what makes an unrecoverable delete acceptable in a module whose predecessor had no audit trail at all. A `month.delete` event that records only the id has thrown the data away.

---

## 5. Endpoints (see `openapi.yaml` 0.8.0) + role gating

`http.go`, mounted on the authenticated `/api` router:

```go
func (h *Handler) Mount(r chi.Router) {
	r.Get("/finance/months", h.list)
	r.With(httpx.RequireWrite).Post("/finance/months", h.create)
	r.Get("/finance/months/{id}", h.get)
	r.With(httpx.RequireWrite).Patch("/finance/months/{id}", h.update)
	r.With(httpx.RequireWrite).Delete("/finance/months/{id}", h.delete) // permanent; no hard/soft distinction
}
```

Reads are **ungated beyond the session** — every member including `reader` (D84). Writes, **delete included**, take the ordinary `RequireWrite` gate. There is **no admin-only route in this module at all**; it is not `admin`/`logging`.

---

## 6. Module registration — `module.go` + wiring

```go
//go:embed migrations/*.sql
var MigrationsFS embed.FS

type Module struct {
	handler *Handler
	widgets []registry.WidgetProvider
	metrics *metricProvider
	lists   *listProvider
}

func (m *Module) Name() string                        { return "finance" }
func (m *Module) RegisterRoutes(r chi.Router)         { m.handler.Mount(r) }
func (m *Module) Migrations() fs.FS                   { return MigrationsFS }
func (m *Module) AuditActions() []string              { return []string{"month.create", "month.update", "month.delete"} }
func (m *Module) Widgets() []registry.WidgetProvider  { return m.widgets }
func (m *Module) MetricProvider() metrics.Provider    { return m.metrics }
func (m *Module) ListProvider() lists.Provider        { return m.lists }
```

**Exact wiring edits** (these are the only files outside `modules/finance/` the backend touches):

| File | Edit |
|---|---|
| `internal/bootstrap/bootstrap.go` | add `{Name: "finance", FS: finance.MigrationsFS}` to `MigrationSources()`; add `MigrationSourcesWithSeed()` / `MigrationFSWithSeed()` (§7) |
| `cmd/home/main.go` | `financeSvc := finance.NewService(sqldb, sink, notify)` · `financeMod := finance.NewModule(financeSvc)` · add `financeMod` to `registry.CollectWidgets([…])` · add to `featureModules` · add to `metrics.Collect(…)` and `lists.Collect(…)` · switch the migration call to `bootstrap.MigrationFSWithSeed()` |
| `internal/platform/audit/audit.go` | add **`ModuleFinance = "finance"`** to the module-identifier constants — every other module passes the constant, not a string literal |
| `internal/modules/admin/listener.go` | add **`case audit.ModuleFinance: return "/finance"`** to `inAppURL`. Without it a `finance.month.*` trigger push opens the dashboard instead of Finance — the service worker navigates to whatever this returns and derives no route of its own |

That is the whole backend surface outside `modules/finance/`. The **backend** side is registry-driven: the widget joins the catalog, the metric/list keys reach the admin composer, and `finance.month.*` joins the action catalog the trigger composer reads — no host edit beyond the table above.

**Three host-side maps are NOT registry-driven, and each silently no-ops if skipped** (all three are in §8's table): the **widget component registry** (`platform/widgets/registry.tsx` — an unknown key renders nothing), the **log browser's module filter** (a hardcoded array), and the **push deep-link map** (`admin/listener.go`'s `inAppURL`, above). `internal/arch`'s `TestModulesDoNotImportEachOther` must stay green — it forbids module→module imports; there is no import allowlist, and `registry` is itself `platform/registry`.

### Widget provider — `rozpocet.go` (FR-F7, D88)

```
Key() = "finance.rozpocet"   Title() = "Rozpočet měsíce"   Module() = "finance"
Description() = "Rozdělení příjmů aktuálního měsíce"
DefaultSize() = "narrow"     AdminOnly() = false
```

`Data(ctx, u)` returns **one of two states**, both from `store.ForMonth(currentMonth)`:

- **recorded** → `{state: "recorded", month, total_income, personal_kaja, personal_andy, needs, savings}` (savings = fun + no-fun);
- **missing** → `{state: "missing", month}` → the UI renders "Zadat ⟨srpen 2026⟩" linking to the add form.

The missing state is the reason the widget exists. Do not treat it as an empty state to be styled quietly — it is the module's most useful output.

*(Key is `finance.rozpocet`, **not** `finance.tento-mesic`: `events` already publishes "Tento měsíc", and two identically-titled catalog rows would be a usability bug.)*

### Metric + list providers — `metrics.go`, `lists.go` (D89/D90)

All four metrics are `ScopeHousehold` — a household budget has no per-recipient value, so none of them take the `userID`.

| Key | Label | Unit | Value |
|---|---|---|---|
| `finance.total_income_current` | Celkový příjem tento měsíc | Kč | current month's `total_income`, else 0 |
| `finance.savings_current` | Spoření tento měsíc | Kč | current month's `fun + no_fun`, else 0 |
| `finance.missing_months` | Nezadané měsíce | měsíců | months from the earliest recorded through the current with no live row |
| `finance.months_recorded` | Zadaných měsíců | měsíců | count of recorded months |

The list is `finance.missing_months` — same key, same selection, `Empty: "nic nechybí"`, `Scope: lists.ScopeHousehold`, items = Czech month labels in ascending order. **Both must call the same store function** (`RecordedMonths`) and derive from the same gap computation, so the count and the list can never disagree (D77's rule).

"Current month" is `time.Now().In(cfg.Timezone)` formatted `2006-01`; the providers receive `asOf` — use it, do not call `time.Now()` inside them, or a scheduled summary firing at 00:01 will disagree with the widget.

**What this unlocks** (worth saying to Karel when it ships): a v5 schedule on day 1 at 09:00, audience all, condition `finance.missing_months gt 0`, body `Chybí zadat: {{list.finance.missing_months}}` — silent in every month where nothing is missing.

---

## 7. The seed — historic months from `fin` (PRD D91, §V6-12)

**The problem to avoid:** a seed migration in the normal sequence would land thirty months of real household finances in **every test database**, because `testsupport.NewDB` builds its schema from `bootstrap.MigrationFS()`. Every `finance` test — and any other module's test that counts rows — would then be written against production data.

**The fix:** the seed is its **own migration source**, and the schema-only assembly stays the default.

```
internal/modules/finance/seed/
    embed.go                        // //go:embed migrations/*.sql → var MigrationsFS
    migrations/09900_finance_seed.sql
```

```go
// bootstrap.go
// MigrationSources is the SCHEMA. It is what tests migrate with.
func MigrationSources() []registry.MigrationSource { /* … + {Name: "finance", FS: finance.MigrationsFS} */ }

// MigrationSourcesWithSeed adds the one-off historic-data seed carried over from
// the retiring `fin` service (PRD D91). ONLY the server entrypoint uses this:
// tests must migrate a schema, not a household's finances.
func MigrationSourcesWithSeed() []registry.MigrationSource {
	return append(MigrationSources(), registry.MigrationSource{Name: "finance-seed", FS: financeseed.MigrationsFS})
}

func MigrationFS() (fs.FS, error)         { return registry.MergeMigrations(MigrationSources()) }
func MigrationFSWithSeed() (fs.FS, error) { return registry.MergeMigrations(MigrationSourcesWithSeed()) }
```

`cmd/home/main.go` calls `MigrationFSWithSeed()`; `testsupport` keeps calling `MigrationFS()`. **Default = no seed** is deliberate: a future caller that forgets about this gets the safe behaviour.

**The seed file** — generated, not hand-written, and committed (it is data, and it must be reviewable in a diff). **It already exists**: `services/home/v6-seed/09900_finance_seed.sql`, generated 2026-08-17 from the live export, 15 rows (`2025-06`…`2026-08`), splits re-derived and matched, applied and re-applied against the `09001` schema to prove idempotency. Copy it in; do not re-key it.


```sql
-- +goose Up
-- +goose StatementBegin
-- One-off: the months migrated from the retiring `fin` service (PRD D91, §V6-12).
-- Generated from fin's live GET /months on <DATE>; N rows. ids and timestamps are
-- PRESERVED so a row is traceable back to fin. created_by is NULL — fin recorded no
-- actor. INSERT OR IGNORE against ux_finance_months_month: applying this to a
-- database that already holds the months is a no-op, not a duplicate-key failure.
INSERT OR IGNORE INTO finance_months
  (id, month, income_kaja, income_andy, rate_personal, rate_operational, rate_fun, rate_nofun, created_by, created_at, updated_at)
VALUES
  ('0190f3…','2025-01', 60000, 40000, 20, 60, 10, 10, NULL, '2025-01-31T18:04:11.221Z', '2025-01-31T18:04:11.221Z'),
  …;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deliberately empty: this migration carries the only copy of history that lives
-- in home. Removing rows on a Down would be a data-loss footgun, and the schema's
-- own Down already drops the table.
-- +goose StatementEnd
```

**Generator guard rails** (fail the generation, not the migration): every row's four rates sum to exactly 100; incomes are non-negative integers; `month` matches the pattern and is unique across the file; the emitted row count equals the export's length. See §12 for how the export is taken.

---

## 8. Frontend — page, widget, nav

```
src/modules/finance/
    widgets/RozpocetWidget.tsx        # both states; "missing" links to the add form
src/routes/finance/
    FinancePage.tsx                   # list + modals (the Months.tsx equivalent)
    MonthFlow.tsx                     # the three-stage flow visualisation
    MonthFormModal.tsx                # add/edit with live preview
    split.ts                          # frontend mirror of the locked formula (for the preview only)
```

**`split.ts` is a mirror, not a second source of truth.** It exists so the form can preview a split before saving, exactly as `fin`'s `frontend/src/lib/split.ts` did. Port it from there with its test, and keep the comment saying which Go file it mirrors. Everything rendered in the list and the widget comes from the **server's** `split`, never from this.

**The flow visualisation** (`fin`'s `FlowViz.tsx`) is worth porting closely — it is the reason the app is nicer than a spreadsheet. Three stages, arrows between them:

1. **Příjem** — Kája's and Andy's incomes.
2. **Osobní** — what each keeps (`personal_*`), each card carrying a "Zbytek → Kandy" line (`to_operational_*`).
3. **Společné účty** — **Provozní účet (Kandy) · potřeby** (`needs`, with "přijato ⟨operational_received⟩, spoření odchází"), **Zábavné spoření** (`fun_savings`), **Nezábavné spoření** (`no_fun_savings`).

Below it, the reconciliation note in Czech: *"Potřeby pohlcují zaokrouhlení, takže součty za osobu i za účet sedí přesně na ⟨celkový příjem⟩ Kč."*

**Colour:** the four buckets use the theme's existing categorical palette — `--c1` osobní · `--c2` potřeby · `--c3` zábavné spoření · `--c4` nezábavné spoření. Both light and dark values are already in `theme/globals.css` (the Log's stats bars already cycle through them, so this is a second consumer, not a first). Do not introduce new hex values and do not hardcode colours in components (`fin`'s `--c-personal` etc. do not come across).

**Formatting:** `monthLabel()` in `i18n/format.ts` already renders `2026-08` → "srpen 2026". Money is `fmtNumber(v) + ' Kč'`. Nothing new is needed in `format.ts`.

**Wiring edits:**

| File | Edit |
|---|---|
| `src/app/routes.ts` | `finance: '/finance'` |
| `src/app/AppShell.tsx` | add to `OVERFLOW` (**not** `PRIMARY`, **no** `adminOnly`), icon `Wallet`, `desc: cs.nav.financeDesc` |
| `src/App.tsx` | register the route |
| `src/api/keys.ts` | `financeMonths: ['finance','months'] as const` |
| `src/api/ws.ts` | a `financeModule` `LiveModule` (`route: routes.finance`, `keys: [['finance']]`, toast `cs.live.financeUpdated`) + `if (type.startsWith('finance')) return financeModule` in `classify` |
| `src/platform/widgets/registry.tsx` | **`'finance.rozpocet': RozpocetWidget`** — the frontend mirror of the backend widget catalog. **An unknown key renders nothing**, so without this the widget stays invisible however correctly the provider is registered |
| `src/routes/log/LogPage.tsx` | add `'finance'` to the hardcoded `MODULES` filter array. *(It currently reads `['', 'logging', 'todo', 'events', 'notes', 'documents', 'dashboard']` — **`admin` and `platform` are already missing**; add those while you are in the file.)* |
| `src/i18n/cs.ts` | the `finance` block (§9) |

## 9. Czech copy — `cs.ts` additions (PRD D85)

```ts
nav: {
  finance: 'Finance',
  financeDesc: 'Rozdělení měsíčních příjmů',
},
live: {
  financeUpdated: 'Finance byly mezitím upraveny',
},
finance: {
  title: 'Finance',
  lede: 'Měsíční příjmy rozdělené na osobní účty, provozní účet a spoření.',
  months: 'Měsíce',
  add: 'Přidat měsíc',
  edit: 'Upravit měsíc',
  income: 'Příjem',
  incomeKaja: 'Příjem Kája',
  incomeAndy: 'Příjem Andy',
  totalIncome: 'Celkový příjem',
  rates: 'Sazby',
  ratePersonal: 'Osobní',
  rateOperational: 'Provozní',
  rateFun: 'Zábavné spoření',
  rateNoFun: 'Nezábavné spoření',
  ratesMustSum: 'Sazby musí dát dohromady 100 %.',
  stageIncome: 'Příjem',
  stagePersonal: 'Každý si nechá {p} % osobně',
  stageJoint: 'Společné účty',
  operational: 'Provozní účet (Kandy)',
  needs: 'Potřeby',
  restToKandy: 'Zbytek → Kandy',
  toSavings: 'Do spoření',
  monthExists: 'Tento měsíc už je zadaný.',
  emptyTitle: 'Zatím žádné měsíce',
  emptyBody: 'Přidejte první měsíc a uvidíte rozdělení příjmů.',
  widgetMissing: 'Zadat {month}',
}
```

Keep **Kandy** — it is the household's own name for the joint account, and translating it away would make the app read as someone else's.

---

## 10. Audit (spine, `HANDOFF-1`) (D86)

- Actions: `month.create`, `month.update`, `month.delete` — declared in `AuditActions()`, qualified `finance.month.*` by the log browser and the v5 trigger composer.
- Entity type **`finance_month`** joins the field-diff set: `month`, `income_kaja`, `income_andy`, and the four rates.
- Czech `summary` strings — "Přidán měsíc srpen 2026", "Upraven měsíc srpen 2026", "Smazán měsíc srpen 2026" — because a v5 trigger rule's default body **is** the summary (D55). Write them as the sentence you would want to receive as a notification.
- The audit write is **inside the same transaction** as the row change. No exceptions in this module — there is no per-user view state here to justify one.

## 11. Security

No new surface, and nothing to configure. Reads member-gated, writes `editor`/`admin` + CSRF, hard delete `admin`. **Money never becomes a float in storage or transport** — integers end to end; the only `float64` in the system lives inside `Compute` between the multiplication and `math.Round`, which is exactly where `fin` put it.

## 12. Tests

- **`split_test.go`** — ported verbatim from `fin`: `TestWorkedExample` (`60 000` / `40 000`, `20/60/10/10`), `TestInvariants` (**six** assertions — the four the PRD names plus `opReceived == sum of transfers` and `total == k + a` — across **11** fixtures), and `TestRatesSum`.
- **Property test** (new): random incomes `0…10 000 000` × random rate quadruples summing to 100, asserting all four invariants for every case. The invariants are what "the totals reconcile" actually means; the worked example alone would not catch a mis-ordered rounding.
- **Odd-money cases**: incomes that make `income × p/100` land on `.5`, and a `T` whose `f`% is fractional — the cases where "which value absorbs the rounding" is observable.
- **Negative `needs`, both forms**: `(6 887 385, 2 030 905, 90/0/5/5)` → `-2` (`operational = 0`, realistic income) and `(0, 50, 1/1/1/97)` → `-1` (`operational = 1`, `T×o < 200`). Assert `needs >= -2`, that the invariants still hold, and that **nothing clamps it**; assert the UI renders `0 Kč` with a footnote.
- **Store/service**: duplicate month → 409; re-create the same month after deleting it → 201; partial `rates` → 422; rates ≠ 100 → 422; negative income → 422; audit row written in the same tx with the expected diff; **`month.delete` carries a full-row diff** — assert all seven fields are present with their old values, because the row is gone and this test is the only thing guarding the record.
- **Seed exclusion**: a test asserting `SELECT COUNT(*) FROM finance_months` is **0** on a fresh `testsupport.NewDB()`. This is the test that keeps §7's split honest as the codebase changes.
- **Catalog agreement**: `finance.missing_months` metric equals `len(items)` of the list of the same key, over a fixture with gaps.
- **Import-lint** (`internal/arch`) stays green.
- **Frontend**: `split.ts` mirror test (same worked example); form blocks submit until rates sum to 100; widget renders both states.

---

## 13. Migrate, verify, retire (PRD §V6-12) — the half that isn't code

**Do not reorder these.** Each step assumes the previous one passed. **Steps 1–2 are already done** — see `services/home/v6-seed/README.md`; start at step 3.

1. **Export** from the **live** `fin`, not a backup: `POST /login` (site `fin`, as Kája or Andy) for an `access_token`, then `GET https://fin.tilcer.cz/months` with `Authorization: Bearer <token>` — `/months` sits behind `RequireBearer`, so a cookie session alone returns 401. Keep the raw JSON as provenance. A `sqlite3 .dump` of the volume is a useful cross-check, but the API response is the reference because it is what the users have been looking at.
2. **Generate** `09900_finance_seed.sql` (§7) with the guard rails, and commit both the generator and its input.
3. **Deploy Home v6 with `fin` still running.** Both live. `fin` stays the reference until step 4 passes.
4. **Verify — the gate (D97).** A throwaway script comparing `fin`'s `GET /months` against Home's `GET /api/finance/months`, after mapping camelCase→snake_case: same id set and count · `month`, both incomes and all four rates equal · **all nine computed split values equal** · `created_at` preserved. Comparing inputs alone is not enough — a mis-ported formula is the one mistake this migration can actually make, and it would pass an inputs-only check.
   **Any mismatch stops the retirement.** With no redirect standing behind the cutover (D96), this is the only safety net — do not stop `fin` until it passes clean.
5. **Retire, in order (D96):**
   0. **Tell Kája and Andy the app has moved** to `home.tilcer.cz/finance`, and have them remove any `fin.tilcer.cz` phone shortcut. There is **no redirect** (D96), so this is the whole comms plan and it happens *before* the host goes dark.
   1. Stop the `fin` **backend and frontend** Coolify apps and release the `fin.tilcer.cz` route — **take a final Litestream snapshot first**.
   2. **Retain** the `fin/` R2 prefix. It is the seed's provenance and costs nothing.
   3. Archive `ws-tilcer-fin` — **after** step 6.
   4. Deprovision the `fin` auth site + its two `single_site` accounts, and drop **`FIN_AUTH_SERVICE_SECRET` from `fin`'s own Coolify env** — it is fin's variable, not auth's. ⚠ **Do NOT touch auth's `AUTH_SERVICE_SECRET`**: that is the shared BE→BE secret `home`, `status` and `karel` also authenticate with, and removing it would break all of them. Last, because it is the one step that would prevent re-running the verification.
   5. `REGISTRY.md`: `fin` → **retired** (with the date and a pointer to home's `finance`), `home` → v6 / 0.8.0.
6. **Recover `fin`'s documents (D98)** — `services/fin/` in the project folder is **empty**; `fin`'s PRD, OpenAPI spec and handoff exist only inside the repo about to become read-only. Copy `handoff/PRD.md`, `handoff/openapi.yaml`, `handoff/HANDOFF.md` into `services/fin/`, marked *retired — superseded by home v6*, before archiving.

---

## 14. Definition of done

- All PRD §V6-11 acceptance criteria pass; endpoints conform to `openapi.yaml` 0.8.0.
- **The split matches `fin` exactly** — worked example + property test, and step 13.4's live comparison including every split value.
- **Delete is permanent and audited** — the row is gone, `month.delete` carries a full-row diff, and the confirm dialog says so.
- `finance` registers through `registry.Module`; import-lint green; migrations run `… admin(08) → finance(09)` and apply on an empty DB and after a Litestream restore.
- **A fresh `testsupport.NewDB()` contains zero months** — the seed source is production-only.
- Widget in the catalog for every role, both states rendering; four metrics + one list in the admin composer; `finance.month.*` in the log filter.
- Frontend complete in Czech per §9; the four buckets use `--c1…--c4` with no new colour values; Finance in "více" for all roles; live-sync toast works.
- `fin` retired in order, its documents recovered, `REGISTRY.md` updated.

## 15. Module packaging

```
backend/internal/modules/finance/
    module.go        # registry.Module: routes, migrations, audit actions, widget
    http.go          # chi mount + handlers, role gates
    service.go       # validation, WithTx + audit-in-tx, notify
    store.go         # SQL
    split.go         # THE LOCKED FORMULA (ported from fin)
    split_test.go    # ported + property test
    types.go         # Month, MonthInput, Rates, Split (snake_case JSON tags)
    metrics.go       # 4 household metrics
    lists.go         # finance.missing_months
    rozpocet.go      # the Nástěnka widget provider
    finance_test.go  # store/service/handler tests
    migrations/09001_finance.sql
    seed/
        embed.go
        migrations/09900_finance_seed.sql
```

Build the formula first, the seed plumbing before the frontend, and the retirement only after the verification passes.
