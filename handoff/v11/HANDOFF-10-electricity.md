# Home — Module 10: `electricity` (Elektřina)

> Build brief for Claude Code. Behaviour: `V8-electricity-brief.md` (**D133–D162**) → `PRD.md` §V8-1…§V8-9. Data shapes: `openapi.yaml` **0.10.0**, tag `electricity`. Read `HANDOFF.md` §3 (module registry) and `HANDOFF-1-logging.md` (the audit spine) first, then **`HANDOFF-8-finance.md`**: this module has finance's shape — a CRUD shell around one locked pure formula — and `fin`'s `split.go` discipline is what `compute.go` inherits.
>
> **The leanest module Home has.** Five tables against garden's eleven; no widget, metric, list, push, scheduler job, blob, seed, outbound HTTP or derived storage (D147, D152). The difficulty is neither volume nor wiring; it is concentrated **entirely in `compute.go`**, in the boundary arithmetic, where an off-by-one day is invisible until the vyúčtování arrives a year later disagreeing by 300 Kč.
>
> **D157–D160 came out of this handoff pass** (brief §7a) and are cited inline. **D161–D162 came out of the implementation-planning pass** (brief §7b) and amended `openapi.yaml` in place: `summary.cost_total_haler` and `.balance_haler` are **nullable and null — never 0 — in `insufficient_data` and `blocked`**, and `summary.headroom` gained **`kwh_mix_dkwh`**. Both are cited inline too. Where this document and the brief disagree, the brief wins.

## The model in one paragraph

**A reading is the state of the meter at 00:00 of its `read_on` (D134).** Every boundary rule restates that: consumption of day *d* = `reading(d+1) − reading(d)`; an interval between readings on `d1` and `d2` covers days `d1 … d2−1`; a ceník effective from `D` prices days `D, D+1, …` until the next version starts (D136 — no stored end date); a zúčtovací období `[starts_on, ends_on]` is written inclusively the way a human writes it (24. 6. 2026 – 23. 6. 2027), requires an **opening reading dated `starts_on`** (D140), and is *closed* by the reading dated **`ends_on + 1`** — simultaneously the next period's opening reading, and whose arrival makes the period entirely actual (D157). One reading, one instant, two periods. On top sit a ceník (three numbers, s DPH a distribucí, used as typed — D135), a záloha schedule with a `due_day` plus optional real payments (D144), and a predikce averaging from the opening reading to the **latest reading**, forecasting every remaining day at the ceník effective on that day (D141, D142). The output is **nedoplatek / přeplatek** and the **doporučená záloha** that lands the period at zero (D146).

Internally every window is **half-open `[from, to)`**; convert the inclusive `ends_on` to `ends_on + 1` exactly once at the edge of `compute.go`. Half the plausible bugs here are one function using inclusive bounds while its caller uses half-open.

## Build order

Do not deviate.

1. **`compute.go`** — pure. No `database/sql`, no `net/http`, no `time.Now()`. Written against brief §4.5 and §4.6 until both land on the numbers §7 pins to the haléř.
2. **`compute_test.go`** — alongside step 1, not after it.
3. **`migrations/11001_electricity.sql`** — the module's only migration (D152).
4. **`store.go`** → 5. **`service.go`** → 6. **`http.go`** → 7. **`module.go` + wiring** → 8. **frontend + `cs.ts`**.

Why compute first: once endpoints exist, "fix the rounding" is a change re-verified through four screens; before they exist it is a change to one function with a test that says whether it is right. `fin` shipped **two contradictory implementations** of its split — the failure this discipline exists to prevent.

---

## 1. Data model — five tables, block 11

`internal/modules/electricity/migrations/11001_electricity.sql`, applied last in the one Goose sequence (`… finance(09) → garden(10) → electricity(11)`, see `bootstrap.MigrationSources`). **No seed source** (D152) — nothing to exclude from `testsupport`, nothing to add to `MigrationSourcesWithSeed()`; do not add one for symmetry.

House conventions: UUIDv7 `id`, `deleted_at` soft delete, `created_by`/`created_at`/`updated_at`, dates `TEXT` `YYYY-MM-DD` in `HOME_TIMEZONE`. **No lexorank `position`** — every collection is chronological, and a draggable order over dates would be a second, contradictory truth. Below `«audit»` = `deleted_at TEXT, created_by TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL` and `«date»` = `GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'`; write both out in the real migration.

```sql
-- +goose Up
-- +goose StatementBegin

CREATE TABLE electricity_readings (        -- energy in TENTHS OF kWh (D148)
    id TEXT PRIMARY KEY,
    read_on TEXT NOT NULL,
    vt_dkwh INTEGER NOT NULL CHECK (vt_dkwh >= 0),
    nt_dkwh INTEGER NOT NULL CHECK (nt_dkwh >= 0),
    note TEXT, «audit», CHECK (read_on «date»)
);
-- Partial, not a table UNIQUE: a soft-deleted reading must not hold the date hostage.
CREATE UNIQUE INDEX ux_electricity_readings_day
    ON electricity_readings (read_on) WHERE deleted_at IS NULL;

CREATE TABLE electricity_tariffs (         -- three numbers, s DPH a distribucí (D135)
    id TEXT PRIMARY KEY,
    effective_from TEXT NOT NULL,          -- end DERIVED from the next row, never stored (D136)
    price_vt_haler    INTEGER NOT NULL CHECK (price_vt_haler >= 0),     -- Kč/MWh in haléře
    price_nt_haler    INTEGER NOT NULL CHECK (price_nt_haler >= 0),     -- Kč/MWh in haléře
    monthly_fee_haler INTEGER NOT NULL CHECK (monthly_fee_haler >= 0),  -- Kč/měs in haléře
    note TEXT, «audit», CHECK (effective_from «date»)
);
CREATE UNIQUE INDEX ux_electricity_tariffs_from
    ON electricity_tariffs (effective_from) WHERE deleted_at IS NULL;

CREATE TABLE electricity_advances (        -- versioned like the ceník
    id TEXT PRIMARY KEY,
    effective_from TEXT NOT NULL,
    amount_haler INTEGER NOT NULL CHECK (amount_haler >= 0),
    due_day INTEGER NOT NULL CHECK (due_day BETWEEN 1 AND 31),  -- stored RAW, clamped at read (D155)
    note TEXT, «audit», CHECK (effective_from «date»)
);
CREATE UNIQUE INDEX ux_electricity_advances_from
    ON electricity_advances (effective_from) WHERE deleted_at IS NULL;

CREATE TABLE electricity_payments (        -- optional; wins over the schedule for its month (D144)
    id TEXT PRIMARY KEY,
    month TEXT NOT NULL,                   -- 'YYYY-MM'
    amount_haler INTEGER NOT NULL CHECK (amount_haler >= 0),
    paid_on TEXT, note TEXT, «audit»,
    CHECK (month GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]'
           AND substr(month, 6, 2) BETWEEN '01' AND '12')
);
CREATE UNIQUE INDEX ux_electricity_payments_month
    ON electricity_payments (month) WHERE deleted_at IS NULL;

CREATE TABLE electricity_periods (         -- never locked (D139); ends_on expected until confirmed (D153)
    id TEXT PRIMARY KEY,
    starts_on TEXT NOT NULL,
    ends_on   TEXT NOT NULL,
    ends_on_confirmed INTEGER NOT NULL DEFAULT 0,
    invoiced_total_haler   INTEGER,        -- the four D154 figures, all nullable
    invoiced_balance_haler INTEGER,        -- negative = nedoplatek, positive = přeplatek
    invoiced_vt_dkwh INTEGER CHECK (invoiced_vt_dkwh IS NULL OR invoiced_vt_dkwh >= 0),
    invoiced_nt_dkwh INTEGER CHECK (invoiced_nt_dkwh IS NULL OR invoiced_nt_dkwh >= 0),
    invoiced_at TEXT, note TEXT, «audit»,
    CHECK (starts_on «date»), CHECK (ends_on «date»), CHECK (ends_on >= starts_on)
);
CREATE INDEX idx_electricity_periods_start
    ON electricity_periods (starts_on) WHERE deleted_at IS NULL;
-- +goose StatementEnd
```

**No FTS**, and a reading's `note` is plain text, length-capped, **not Markdown** — do not route it through the `notes` sanitiser. Four rules cannot be CHECKs — three are cross-row, one would destroy user intent — and all live outside the DDL:

**Monotonicity, in `service.go`, inside the tx.** On create, and on update when `read_on`/`vt_dkwh`/`nt_dkwh` changed: find `prev` = newest live reading with `read_on < d` and `next` = oldest with `read_on > d`, both excluding the row being edited. **422** if `prev` exists and either register is below `prev`'s, or if `next` exists and either of `next`'s is below the new values. **Both sides, always** — checking only `prev` lets a back-filled row break the chain in front of it, and with D150 in force a falling counter is always a typo. A trigger could enforce this but could not name the neighbour, and that message is the point: *"Odečet 12 400 kWh ve VT je nižší než odečet z 1. 8. 2026 (12 640 kWh)."* **Delete needs no such check** — if `a ≤ b ≤ c` then `a ≤ c`; removing a middle reading merges two valid intervals into one. It *can* create a hard block, discovered on read, not prevented on write.

**Period overlap → 422**, guard query in the tx (SQLite has no exclusion constraints): refuse if another live period satisfies `starts_on ≤ other.ends_on AND ends_on ≥ other.starts_on`.

**Tariff delete → 409 (D160), narrower than it sounds.** Deleting a *middle* version legitimately reprices its days and is allowed; the audit event records it. Refuse only when the deletion would strand a day inside a live period — **iff some live period `P` has no version other than this one with `effective_from ≤ P.starts_on`.** Advances and payments have no such guard: a counted month with no schedule and no payment contributes **0 Kč** and renders *"bez předpisu"*. **`due_day` is clamped at read (D155), never on write** — a stored clamp turns a 31 into a 28 the first February and stays wrong.

---

## 2. `compute.go` — the locked computation. **Build this first.**

v8's `split.go`: one pure file, its own test file, taking a loaded snapshot and returning the whole summary. Přehled, Odečty, Historie and `/intervals` all read through it — if any two ever disagree about a number, the bug is unfindable.

### 2.1 Units and the three rounding points

Money in **haléře** (`int64`), energy in **tenths of kWh** (`int64`). **No floats anywhere in the money path** (D148) — not transiently, not "just for the average".

```go
// divRound divides half-away-from-zero. den must be > 0. EVERY rounding in this file
// goes through it; there is no other call to any rounding routine.
func divRound(num, den int64) int64 {
	if num >= 0 { return (num + den/2) / den }
	return -((-num + den/2) / den)
}
```

All money here is non-negative, so half-away-from-zero **is** half-up; the negative branch exists so a future signed input cannot silently truncate. There are exactly **three** rounding points, and no fourth may be added.

| # | Where | Formula |
|---|---|---|
| 1 | Energy of one actual interval, one ceník version | `divRound(vt_dkwh·price_vt + nt_dkwh·price_nt, 10000)` |
| 2 | Energy of one forecast run of `n` days, one version | `divRound(n·(dVT·price_vt + dNT·price_nt), elapsed_days·10000)` |
| 3 | One fee chunk, keyed (calendar month × version) | `divRound(monthly_fee_haler·days_in_chunk, days_in_month)` |

`10000` is not magic: 1 dkWh = 10⁻⁴ MWh and the price is haléře per MWh. **One rounding on the VT+NT sum** — not per register, not per tariff, not per day. Worst-case magnitude (10⁷ dkWh × 10⁶ haléře × 400 days, doubled) is ~10¹⁶, so `int64` has three orders of magnitude spare; comment the bound and test it rather than reaching for `math/big`. The API returns **haléře**; the frontend rounds to Kč, so `2 156 049` displays as `21 560 Kč`. The only server-side Kč rounding is the doporučená záloha (§2.6).

### 2.2 Snapshot in, summary out

```go
type Snapshot struct {
	Period   Period
	Readings []Reading   // live, ASCENDING by ReadOn, range [starts_on, ends_on+1]
	Tariffs  []Tariff    // live, ASCENDING by EffectiveFrom, ALL of them
	Advances []Advance   // live, ASCENDING by EffectiveFrom, ALL of them
	Payments []Payment
	Today    Date        // resolved by the caller; compute.go reads no clock
}

func Summarize(s Snapshot) Summary
func BuildIntervals(s Snapshot) ([]Interval, *Blocking)     // shared with GET /intervals
func BuildHistory(s Snapshot, from, to Month) []MonthPoint  // D159
```

| Type | Fields |
|---|---|
| `Summary` | ids/dates, `EndsOnConfirmed` · `Blocking []Blocking` (non-empty ⇒ everything below is partial; the **earliest** is the one that truncates `Actual`) · `Actual, Forecast Side` · `Closed bool` (D157) · `EnergyTotalHaler`, `FeeTotalHaler`, `CostTotalHaler *int64` · `LastReading`, `LastReadingAgeDays` (the D156 line) · `ElapsedDays`, `RemainingDays` · `AvgVT/NTMdkwhPerDay` (thousandths of dkWh/day, **display only, never priced**) · `Months []CountedMonth` · `AdvancesTotalHaler`, `AdvancesDueHaler`, `BalanceHaler *int64` · `RecommendedKc *int64` (nil when no months remain) · `Headroom *Headroom` (only when the forecast is impossible) · `Invoiced *InvoiceComparison` |
| `Side` | `FromOn`, `ToOn` (half-open), `Days` · `VTDkwh`, `NTDkwh` · `EnergyHaler` (**authoritative**) · `EnergyVTHaler`, `EnergyNTHaler` (the D158 split) · `FeeHaler` · `CostHaler` |
| `Interval` | window, `Days`, `VTDkwh`, `NTDkwh`, `TariffID`, `EnergyHaler` — **energy only; an interval carries no fee** (§2.4) |
| `Blocking` | `Kind` · `MissingOn` (pre-fills the reading form — the date only, never the values, D137) · `IntervalFrom`, `IntervalTo` · `MessageCS` |
| `CountedMonth` | `Month`, `AmountHaler`, `Source` (`payment`\|`schedule`\|`none`), `DueOn`, `Due bool`, `PaidOn *Date` |
| `Headroom` | `AdvanceHaler`, `FeeHaler`, `ForEnergyHaler`, `KwhAllVT`, `KwhAllNT`, `KwhMix` (whole kWh; all three go on the wire as `*_dkwh`, **D162**) |

⚠ **`CostTotalHaler` and `BalanceHaler` are pointers, and nil — not 0 — whenever the totals are not produced (D161):** `insufficient_data` and `blocked` alike. The wire declares both nullable to match. Two consequences worth stating, because the compiler will not: the zero value of this struct is **not** a valid summary, and `Actual` stays populated when it is valid — the figures *before* a gap do not become unknown because the ones after it are.

Two loading rules are easy to get subtly wrong. **Readings load through `ends_on + 1`, not `ends_on`** — the closing reading is dated the day *after* the period (D134); take the newest in `[starts_on, ends_on+1]` as "the last reading", and when it is dated exactly `ends_on + 1` set `Closed = true` (**D157**). And **all tariffs load**, not just those inside the period — the version governing `starts_on` normally starts well before it.

⚠ **The VT/NT display split (D158).** Two independent roundings can miss `EnergyHaler` by a haléř, and the two lines on screen then fail to add up to the headline above them. **Round VT, then define `EnergyNTHaler = EnergyHaler − EnergyVTHaler`** — one component absorbing the error by subtraction, the `needs` pattern from the fin split. This is the **only** place the remainder technique is used in this module. Assert the identity.

### 2.3 Intervals, the hard block (D137), and actual energy

Walk the readings pairwise; interval *i* is `[r[i].ReadOn, r[i+1].ReadOn)`. Block detection is one predicate: **a ceník `effective_from` strictly inside the interval**, `d1 < effective_from < d2`. Boundary equality is *not* a block — `== d1` means the version governs the whole interval, `== d2` means the change starts where the next interval starts. Evaluate in order:

1. No live reading dated `starts_on` → `missing_opening_reading`, `MissingOn = starts_on`. No money at all (D140); do not fall back to the nearest reading.
2. No tariff with `effective_from ≤ starts_on` → `no_tariff`. Same treatment.
3. A change strictly inside an interval → `tariff_change_inside_interval`, `MissingOn = effective_from`; report the **earliest** if several. `Actual` covers `[starts_on, blocked.FromOn)` and stays valid and visible; `Forecast`, the totals, `BalanceHaler` and `RecommendedKc` are **not produced**.

**The forecast is never hard-blocked** — a future version there is the normal case (D142). The block is a fact about *measurement*, and there is nothing to measure in the future.

Each interval is priced by exactly one version — the latest with `effective_from ≤ interval.FromOn`; there cannot be a second, or it would be a block. `Actual.EnergyHaler = Σ Interval[i].EnergyHaler`, **defined as the sum**, never recomputed from a span-wide delta; that definition is what brief §9/#7 and §7's property test pin.

### 2.4 Poplatky (D143) — chunked by month, belonging to no interval

`chunk_haler` per rounding point 3, keyed **(calendar month × ceník version)**; `FeeTotalHaler = Σ chunks` over `[starts_on, ends_on+1)`. A whole calendar month inside one version costs **exactly** the monthly fee — `fee · d/d`, no rounding possible; pro-rata shows up only at the ends of a period and at a mid-month price change.

⚠ **Fees are not allocated into intervals, and an Odečty row's Kč is energy only, labelled *energie*.** Allocating a chunk across the intervals overlapping it would invent a second rounding rule whose only job is keeping two views agreeing. The period is presented as **energie + poplatky**, which is how the supplier states it too. Brief §9/#7 is therefore two separate identities — `Σ interval energy == EnergyTotalHaler` and `Σ fee chunks == FeeTotalHaler` — not one.

The one place a chunk is still divided is Přehled's **actual/forecast display split**, when the last reading falls mid-month: split by day count, forecast side takes the difference. One subtraction, not a helper; `FeeTotalHaler` is unaffected.

### 2.5 Prediction (D141, D142)

**The boundary between fact and forecast is the latest reading, not today.** Days since the last reading are forecast like any other future day. This is the most commonly mis-implemented sentence in the brief.

```
elapsed_days = last_reading.read_on − starts_on          // must be >= 1
dVT = last.vt_dkwh − opening.vt_dkwh
dNT = last.nt_dkwh − opening.nt_dkwh
```

Split `[last.read_on, ends_on + 1)` into **runs**, one per ceník version effective in that window (D142), price each by rounding point 2, and derive `run_vt_dkwh = divRound(n·dVT, elapsed_days)` for display. Note what this does **not** do: compute a per-day average, round it, then multiply. Both divisions collapse into one rounding — a rounded per-day average times 243 days drifts by tens of koruna. Fees use §2.4 unchanged.

**When prediction is impossible** — fewer than two readings, `elapsed_days < 1`, no ceník effective on or before `starts_on`, or an unresolved block — `Forecast` is the zero value, the totals / `BalanceHaler` / `RecommendedKc` are **not produced**, and `Headroom` is filled instead. **The module never shows a number it hasn't earned** — not a zero, not a spinner, not a dash. When `Closed` is true the opposite holds: the forecast is empty because the answer is known (D157).

### 2.6 Counted months, zálohy, balance, doporučená záloha

**Counting (D145):** `starts_on ≤ month_first ≤ ends_on`, so a year-long period is always exactly 12 months — Karel's **24. 6. 2026 – 23. 6. 2027** counts **červenec 2026 … červen 2027**, and **červen 2026 is not among them**. **The amount (D144):** the payment row if one exists, else the schedule effective on the **1st of that month**, else `0` with `Source = "none"`. **Already due (D155):** `due_on = min(due_day, days_in_month)`, due iff `due_on ≤ Today` — **inclusive**. This is the **only** place `due_day` is read; it moves the doporučená záloha and nothing else — not the period total, not `AdvancesTotalHaler`, not D145's counting.

**Doporučená záloha (D146)** — the one server-side Kč rounding, **up**, floored at 0, omitted when no months remain:

```go
remaining := len(counted) - dueCount
if remaining == 0 { return nil }
num := s.CostTotalHaler - s.AdvancesDueHaler
if num <= 0 { z := int64(0); return &z }
kc := (num + int64(remaining)*100 - 1) / (int64(remaining) * 100)   // ceil-div, haléře → Kč
```

One ceil-div straight from haléře to Kč. Do **not** divide to haléře, round, then convert — the double rounding moves the answer by a koruna in exactly the fixture §7 pins.

`BalanceHaler = AdvancesTotalHaler − CostTotalHaler` (> 0 přeplatek, < 0 nedoplatek); `AdvancesDueHaler` sums only the months whose `due_on ≤ Today`.

### 2.7 Headroom (brief §4.6), history (D138/D159), purity

**Headroom** is filled only when the real prediction is unavailable, so the first screen Karel ever sees carries a real number instead of an empty panel. `ForEnergyHaler = advance − monthly_fee` from the versions effective today; `KwhAllVT = divRound(ForEnergyHaler · 10000, price_vt_haler) / 10`; `KwhMix` blends at **30 % VT / 70 % NT**, a stated heuristic named as such in the Czech copy, never presented as measured. Against Karel's numbers this must give **857,65 Kč ≈ 176 kWh** (vše VT) · **213 kWh** (vše NT) · **~200 kWh** (30/70). Pin all four.

⚠ **The mix is ONE division, not a blended price rounded and then divided into** (**D162**): `KwhMixDkwh = divRound(ForEnergyHaler · 100000, 3·price_vt_haler + 7·price_nt_haler)`. Rounding `(3·vt + 7·nt)/10` to a price first, then dividing, moves the answer by a whole kWh. Karel's numbers: `divRound(8 576 500 000, 4 276 278) = 2006` → **200 kWh** ✓. It is **served, not derived on the client** — the summary carries no prices, so a client reconstructing them from `KwhAllVT`/`KwhAllNT` would land somewhere else than this test. Note also that this is the fourth `divRound` call outside the money path: §2.1's "exactly three rounding points" governs **money**, and headroom kWh is a display figure that no Kč ever passes through.

**`BuildHistory`** lives in this file so it cannot become a second interval walk. kWh per month spread each interval's kWh evenly across its days — display only, labelled *"přibližné"* (D138). Kč per month (**D159**) is the interval's already-exact cost divided across the months it spans in proportion to days, never a repricing of interpolated kWh; fee chunks are already per month and need no division. So the kWh columns are approximate and say so, the Kč columns are exact totals cut along an approximate line, and no price ever touches an invented meter value.

**`compute_purity_test.go`** parses `compute.go` with `go/parser` and asserts its import set is a **subset of a whitelist** — `time`, `fmt`, `sort`, `errors`, `strings` — not a blacklist of `database/sql`, because a blacklist only catches what the test author thought of and what will actually creep in is `net/http` or a store type. Assert in the same test that the file contains **no `time.Now`**: a summary that consults the clock changes while it is being read, and the three views would disagree across a midnight. (Brief §9/#12, and then some.)

---

## 3. Store, service, endpoints

**`store.go`** — ordinary SQL, soft-delete filters everywhere. One read carries weight: **`Snapshot(ctx, periodID)`** — five queries, no N+1 (the period; readings in `[starts_on, ends_on+1]` ascending; all tariffs; all advances; payments for the counted months), resolving `Today` from `HOME_TIMEZONE` **once**. `Summarize`, `BuildIntervals` and `BuildHistory` consume that same struct — three endpoints, one load path, one truth. The rest is CRUD with date keyset pagination.

**`service.go`** — validate → `WithTx` → write **+ audit in the same transaction** → notify after commit.

- ⚠ `audit.Sink.Record(ctx, tx, audit.Event{…})`. The type is **`audit.Event`**, not the live-sync notifier's **`Entry`** — two different structs on two different paths, and the compiler error when you mix them is unhelpful enough that people "fix" it by reaching for the wrong one. Audit is in the tx; the notifier fires after commit.
- **Field-level diffs on all five entities** — "who changed the VT price and to what" is what this module's Log entries exist to answer. The §1 guards apply: monotonicity, period overlap, D160's tariff-delete predicate.
- **`ends_on` default (D153):** if omitted on create, `starts_on + 1 year − 1 day` with `ends_on_confirmed = false`. Correcting it later is one `PATCH` field and every number follows on the next read.
- **Nothing is ever locked (D139).** No closed-period guard, no 409 on editing a period with an invoice recorded, no admin tier. Do not add one; the audit spine is the compensating control, exactly as in finance.

**Endpoints — 13 paths, tag `electricity`** (brief §5 verbatim; do not invent others — no `/recompute`, `/close` or `/import`). Reads: any authenticated member, `reader` included. Writes: `editor`/`admin` + CSRF. **No admin-only route** (D151), delete included.

| Method | Path | Notes |
|---|---|---|
| `GET` `POST` · `PATCH` `DELETE` | `/api/electricity/readings` · `/{id}` | newest-first; `POST`/`PATCH` validate monotonicity (422); soft delete |
| `GET` `POST` · `PATCH` `DELETE` | `/api/electricity/tariffs` · `/{id}` | delete → **409** only per D160 |
| `GET` `POST` · `PATCH` `DELETE` | `/api/electricity/advances` · `/{id}` | a mistyped amount or due day must be fixable |
| `GET` `POST` · `PATCH` `DELETE` | `/api/electricity/payments` · `/{id}` | one row per `YYYY-MM` |
| `GET` `POST` · `PATCH` `DELETE` | `/api/electricity/periods` · `/{id}` | overlap → **422**; `PATCH` carries `ends_on`, `ends_on_confirmed`, the four invoiced fields |
| `GET` | `/api/electricity/summary?period_id=` | the computed picture incl. `blocking`, `closed`, counted months, headroom |
| `GET` | `/api/electricity/history?from=&to=` | per-month kWh + Kč series; `from`/`to` are **`YYYY-MM`** |
| `GET` | `/api/electricity/intervals?period_id=` | kWh, **energy Kč** and the pricing ceník — the "show your work" view behind Odečty |

⚠ **Cursors are dates, not UUIDv7 (D149)** — `read_on` · `effective_from` (tariffs, advances) · `starts_on` · `month` (payments, the finance precedent). Declared **inline**, never `$ref`-ing the shared UUIDv7 `Cursor`: an id passed to a `read_on` keyset is compared lexically against a date and silently returns a wrong or empty page. Malformed → **422**, never a silent re-serve of page one. `next_cursor` documented inline as the matching natural key; `Limit` is the shared component, unchanged.

⚠ **The v7 YAML rule:** an inline `{ … }` `description:` containing a comma **must be quoted** or YAML eats the tail as a stray null key — exactly the shape those five cursor declarations need. Bump `info.version` to **0.10.0**, add the tag, reuse the shared `Error`/`Limit`/`401|403|404|409|422`/security components.

---

## 4. Module registration — `module.go` + wiring

```go
//go:embed migrations/*.sql
var MigrationsFS embed.FS

func (m *Module) Name() string                { return "electricity" }
func (m *Module) RegisterRoutes(r chi.Router) { m.handler.Mount(r) }
func (m *Module) Migrations() fs.FS           { return MigrationsFS }
func (m *Module) AuditActions() []string {
	return []string{
		"reading.create", "reading.update", "reading.delete",
		"tariff.create",  "tariff.update",  "tariff.delete",
		"advance.create", "advance.update", "advance.delete",
		"payment.create", "payment.update", "payment.delete",
		"period.create",  "period.update",  "period.delete",
	}
}
```

Fifteen actions, all `editor`/`admin`, all with field-level diffs, **none with a `system` actor** — there is no background job, so nothing here can be written by anything but a person.

**What `module.go` deliberately does NOT have** (D147, D152): **no `Widgets()`, no `MetricProvider()`, no `ListProvider()`**, no scheduler registration, no push, no blobstore. If `registry.Module` makes any of those mandatory, return **`nil`** — not an empty non-nil slice, which would put an empty section in the admin composer. Do **not** add `electricity` to `registry.CollectWidgets`, `metrics.Collect` or `lists.Collect`.

| File | Edit |
|---|---|
| `internal/bootstrap/bootstrap.go` | add `{Name: "electricity", FS: electricity.MigrationsFS}` to `MigrationSources()`. **Nothing for `MigrationSourcesWithSeed()`** — there is no seed source |
| `cmd/home/main.go` | `elecSvc := electricity.NewService(sqldb, sink, notify, cfg)` · `elecMod := electricity.NewModule(elecSvc)` · add to `featureModules` **only** |
| `internal/platform/audit/audit.go` | add **`ModuleElectricity = "electricity"`** to the module constants — every module passes the constant, never a literal |
| `internal/modules/admin/listener.go` | add **`case audit.ModuleElectricity: return "/elektrina"`** to `inAppURL` |

`internal/arch`'s **`TestModulesDoNotImportEachOther` must stay green**; `electricity` imports `platform/*` only, and only `db`, `audit`, `httpx`, `idgen`, `reqctx`, `dates`.

### ⚠ The host-map trap, inverted

Four host-side maps are **not registry-driven** and each silently no-ops if skipped; v5, v6 and v7 all tripped over this. **v8 is the first module where exactly one of them must be left alone** — which makes it the first module where a diligent implementer working from the previous handoff's checklist introduces the bug.

| Map | v8 |
|---|---|
| `AppShell`'s `OVERFLOW` nav list | **add** "Elektřina" → `/elektrina` |
| the Log browser's hardcoded `MODULES` array | **add** `electricity` |
| `admin/listener.go`'s `inAppURL` | **add** `electricity` → `/elektrina` |
| `platform/widgets/registry.tsx` | **do not touch — there is no widget** |

A registry entry for a module with no widget provider produces a dashboard tile that resolves to nothing: no compile error, no runtime error, an empty card. **Three edits, not four** — verify the fourth file is absent from the diff.

---

## 5. Frontend — `src/modules/electricity/`

**Place it in `src/modules/electricity/`, not `src/routes/`.** v6's finance pages landed in `src/routes/finance/*` under the older layout; the repo genuinely has both conventions in it and that is an open cleanup item. Do not add to the legacy side, and do not move finance as part of this work.

React 19 + Vite + TS + Tailwind v4 + shadcn/ui + TanStack Query. Query keys `['electricity', <resource>, …]`; **any mutation invalidates `summary`, `intervals` and `history` together** — a stale summary beside a fresh reading list is worse than a spinner, because it is a number that has quietly stopped being true. **Elektřina joins the "více" overflow for everyone**, no admin gate; the four thumb tabs are untouched. Routes: `/elektrina` (Přehled) · `/odecty` · `/ceniky` · `/historie`.

**The four screens are specified in brief §6; build them as written.** What §6 does not carry:

- **Three states on Přehled, all designed, none a fallback.** *Normal* — the D158 split, so the VT and NT lines sum exactly to the headline, and the total stated as **energie + poplatky**. *Closed* (`closed == true`, D157) — the *"predikce z průměru…"* caveat is replaced by ***skutečnost*** and the invoiced comparison appears. *Empty* — Karel's opening data renders brief §4.6's headroom line instead of a panel. No spinner, no blank panel, and above all **no zero**: a `0 Kč` nedoplatek is a lie that looks like good news, and this is the **first thing Karel will ever see**.
- **An Odečty row's Kč is energy only and is labelled *energie*** (§2.4). Poplatky appear once, on Přehled, as their own line.
- **Whole-kWh inputs (D148).** The wire carries `vt_dkwh`/`nt_dkwh`; the form multiplies by 10 on submit, divides by 10 for display, offers `step="1" inputMode="numeric"` and no decimal separator, and renders `dkwh/10` with no decimal when `dkwh % 10 == 0` and one when it isn't — so a value from a future decimal meter is never silently truncated.
- **The blocked state's button pre-fills the reading form with `blocking.missing_on` — the date only, never the values** (D137).
- Charts: **Path A palette** (`home-design-palette`), VT and NT distinguished by **more than colour**, D138's *"přibližné"* note on every kWh column. **Light theme first, 375 px first.** Offline read-only from the persisted Query cache; live sync toast *"Elektřina byla mezitím upravena"*.

**Czech copy — `cs.ts`.** Fixed vocabulary, verbatim, so pages and Log say the same words: **Elektřina · Odečet · Vysoký tarif (VT) · Nízký tarif (NT) · Ceník · Měsíční poplatky · Záloha · Předpis záloh · Splatnost · Zúčtovací období · Předpokládaný konec · Vyúčtování · Nedoplatek · Přeplatek · Doporučená záloha · Spotřeba · Predikce · Skutečnost · Energie.** Own the rest, as sentences a person would say: the four *"zatím nelze předpovědět"* reasons, each naming what is missing; the blocked and missing-opening-reading lines; the monotonicity refusal with the neighbour's date in it; the headroom sentence and its 30/70 caveat; *"poslední odečet před {N} dny"*; *"spočteno 21 560 Kč · vyúčtováno 21 890 Kč · rozdíl −330 Kč"* and its kWh twin; the D138 chart note; *"bez předpisu"*. Counts need the **three plural forms** (*1 den · 2 dny · 5 dnů*; *1 odečet · 2 odečty · 5 odečtů*). Dates `24. 6. 2026`, money with a non-breaking thousands space and a comma decimal, energy in kWh.

## 6. Audit and security

Every mutation writes an audit event in the same transaction; all five entities are in the **field-diff set**. `electricity.*` must appear in the Log browser filter (the `MODULES` edit, §4) and in the admin trigger composer, which it gets free from `AuditActions()`. No new inbound surface, no outbound HTTP, no new env vars, no secrets. Reads member-gated, writes `editor`/`admin` + CSRF. **No float reaches the database, and after §2 there is no float in the module at all.** Notes are plain text, length-capped, not Markdown.

---

## 7. Tests

`compute_test.go` carries the weight, table-driven throughout.

**Brief §4.5 — the general case.** Period 1. 4. 2026 – 31. 3. 2027; ceník A od 1. 1. 2026 (VT 3 200, NT 2 400 Kč/MWh, poplatky 350 Kč/měs), ceník B od 1. 1. 2027 (3 600 / 2 700 / 380); záloha 1 800 Kč, **`due_day = 15` as the brief now pins**; readings 1. 4. 2026 = 12 000 / 30 000 and 1. 8. 2026 = 12 640 / 31 480; `Today = 2026-08-20`. Values in haléře:

| `Actual.Days` `122` | `Actual.EnergyHaler` `560 000` | `Actual.FeeHaler` `140 000` (4×350) |
|---|---|---|
| `Forecast.Days` `243` (153 A, 90 B) | `Forecast.EnergyHaler` `1 167 049` | `Forecast.FeeHaler` `289 000` (5×350 + 3×380) |
| `EnergyTotalHaler` `1 727 049` | `FeeTotalHaler` `429 000` | `CostTotalHaler` **`2 156 049`** → 21 560 Kč |
| `AdvancesTotalHaler` `2 160 000` | `BalanceHaler` **`+3 951`** → přeplatek 40 Kč | `RecommendedKc` **`1795`** |

⚠ **`due_day` is load-bearing.** At `due_day ≤ 20` five months are due on 20. 8. 2026 and the answer is 1 795 Kč; at `due_day = 25` only four are and it is `ceil(1 436 049 / 800) =` **1 796 Kč**. Keep both as separate cases.

**Brief §4.6 — Karel's opening state.** Ceník od 24. 6. 2026 (VT 485 865, NT 402 669, poplatky 64 235 h); záloha 150 000; období 24. 6. 2026 – 23. 6. 2027 unconfirmed; one reading 24. 6. 2026 = 320 / 700 dkWh. Assert `Blocking` empty, `Forecast` zero, **`CostTotalHaler == nil` and `BalanceHaler == nil`** (D161 — assert nil, *not* 0, and assert it again on the serialized JSON, since a pointer that is nil in Go and `0` on the wire is exactly the bug this shape exists to prevent), `Months` = exactly 12 entries `2026-07 … 2027-06` with **`2026-06` absent**, `AdvancesTotalHaler == 1 800 000`, `Headroom == {ForEnergyHaler: 85 765, KwhAllVT: 176, KwhAllNT: 213, KwhMix: 200}`.

**One-line assertions, one case each:**

- A whole month inside one ceník version → chunk == the fee exactly (`64 235`, not 64 234); all twelve months, both fee values.
- 24. 6. 2026 – 23. 6. 2027 → 12 counted months, `2026-06` and `2027-07` excluded; also a period starting on the 1st (12), a one-day period, a one-month-plus-a-day period (2).
- `due_day = 31` → due 28. 2. 2027, 29. 2. 2028, 30. 4.; changing it 1 → 31 moves `RecommendedKc` and leaves `CostTotalHaler`, `AdvancesTotalHaler` and `Months` byte-identical. `due_day = 15`, `Today` = the 15th → that month **is** due (D155 inclusive).
- With 1. 4. = 12 000 and 1. 8. = 12 640: 1. 6. = 11 900 → 422 naming **1. 4. 2026**; 1. 6. = 12 800 → 422 naming **1. 8. 2026**. Both neighbours, both directions.
- Readings 1. 12. 2026 / 1. 2. 2027 with a ceník change on 1. 1. 2027 → `tariff_change_inside_interval`, `MissingOn = 2027-01-01`, `Actual` ends 1. 12. 2026, no totals. Adding that reading clears it and **every figure dated before 1. 12. 2026 is byte-identical to the blocked run** — that assertion is the test.
- No reading on `starts_on` → `missing_opening_reading`, no money at all. `effective_from == d1` and `== d2` are **not** blocks and price cleanly.
- A future ceník B leaves every `Actual` field and every earlier period's total unchanged and moves only `Forecast.EnergyHaler`; editing ceník A's prices moves only A's days (brief §9/#2).
- **D157:** the reading dated `ends_on + 1` sets `Closed = true`, `Forecast.Days == 0`, `CostTotalHaler == Actual.CostHaler`, and enables the invoiced comparison in Kč and kWh. Changing an unconfirmed `ends_on` re-projects the forecast and changes no actual figure.
- **D158:** `EnergyVTHaler + EnergyNTHaler == EnergyHaler` on every fixture, including one built so two independent roundings would be off by one.
- **D159:** `Σ history[m].CostHaler == CostTotalHaler` over a fully-covered period, and no interpolated kWh is ever passed to a pricing function — assert by construction, since the history builder takes interval costs, not kWh.
- **D160:** deleting the only version effective on or before a period's `starts_on` → 409; a middle version → 204 **and the period's total changes**; a purely-future version → 204.
- **Property test, ~1 000 randomised sequences** (3–20 readings, 1–4 versions, random period bounds and `due_day`): `Σ Interval[i].EnergyHaler == Actual.EnergyHaler`; `Σ fee chunks == FeeTotalHaler`; `EnergyTotalHaler + FeeTotalHaler == CostTotalHaler`; `AdvancesTotalHaler − CostTotalHaler == BalanceHaler`; VT+NT sums to energy on both sides; no panic on any block, empty/closed period or zero-consumption run. Finance's `TestInvariants` in another domain, for the same reason — to make it impossible for a later refactor to introduce a **second** way of totalling the same numbers.
- Overflow guard: absurd-but-legal inputs stay positive and monotone. **`compute_purity_test.go`**: the import whitelist and the no-`time.Now` assertion (§2.7).

**Service / HTTP:** period overlap → 422, adjacent periods (`ends_on + 1 == next.starts_on`) → 201 · `reader` GET → 200 on all six read paths, `reader` write → 403, missing CSRF → 403 · a malformed cursor on each of the five collections → **422**, and a UUIDv7 passed as a `read_on` cursor → 422, not an empty page · every mutation writes exactly one audit event **in the tx**, and a rolled-back write leaves none · `due_day` 0 or 32 → 422, `month = "2026-13"` → 422, `history?from=2026-1` → 422, negative `amount_haler` → 422.

**Architecture:** `TestModulesDoNotImportEachOther` green · an explicit assertion that `electricity` imports **none of** `platform/metrics`, `platform/lists`, `platform/push`, `platform/scheduler`, `platform/blobstore` (the garden precedent, extended — all five are what a later "small improvement" reaches for, and this module's whole shape is the claim that it needs none of them) · an assertion that it registers **no** widget, metric or list provider (D147), so a future refactor cannot quietly add one.

---

## 8. Definition of done

- All brief §9 acceptance criteria (1–13) pass; `openapi.yaml` **0.10.0** validates and the endpoints conform.
- **`compute.go` is pure and is the only implementation of every number in the module.** The import whitelist test is green; nothing else prices an interval, chunks a fee, counts a month or resolves a ceník version.
- The two fixtures land on **2 156 049 h / +3 951 h / 1 795 Kč** and on the brief §4.6 empty state with the **857,65 Kč ≈ 200 kWh** headroom, to the haléř.
- **Exactly three rounding points exist** (§2.1); brief §9/#7's energy and poplatky identities hold separately; the D158 VT+NT parts sum exactly to the energy total. The exact-fee invariant, the hard block resolving without moving an earlier number, the both-neighbours monotonicity refusal, Karel's 12 counted months, the `due_day = 31` clamp, D157's closing-reading flip and D160's narrow 409 all pass as specified in §7.
- Migrations run `… finance(09) → garden(10) → electricity(11)` on an empty DB and after a Litestream restore. **`11001_electricity.sql` is the only migration**; no seed, no `testsupport` exclusion to make.
- `electricity` registers through `registry.Module` — routes, migrations, 15 audit actions — and **nothing else**. Import-lint green; no metrics, no lists, no push, no scheduler, no blobstore, **no widget**.
- **Three** host maps updated (`OVERFLOW`, the Log's `MODULES`, `inAppURL` → `/elektrina`); `platform/widgets/registry.tsx` untouched and **verified untouched in the diff**.
- Every mutation audited in the same transaction with field-level diffs; `electricity.*` in the Log filter and the trigger composer. Live sync toast *"Elektřina byla mezitím upravena"*; offline read-only from cache.
- Frontend: four routes under `/elektrina` in `src/modules/electricity/`, Czech copy complete, whole-kWh inputs, Odečty Kč labelled *energie*, **the empty and blocked states both implemented as designed screens**, VT/NT distinguishable by more than colour, no new colour values, light theme and 375 px checked first.

## 9. Two blanks — ask Karel before the first real data goes in

Both are in brief §8. Neither blocks the build; both block the module being *correct* on day one.

1. **The záloha's due day** — a number **1–31** for the 1 500 Kč záloha. It is `electricity_advances.due_day`, read in exactly one place, and it moves only the doporučená záloha. There is no seed, so do not guess a value into a migration — enter it through the UI. (The **15** in the §7 fixture is the brief's worked example, not Karel's answer.)
2. **The supplier's real period end date.** The period ships as **24. 6. 2026 – 23. 6. 2027, `ends_on_confirmed = false`**, which is correct and is what D153 exists for. When the real date arrives it is one `PATCH` and every number follows on the next read.

⚠ **One thing to check when the first záloha is paid.** Červen 2026 does **not** count toward Karel's period (it does not contain 1. 6. 2026). If a záloha was actually paid in June for this supply, it belongs to the period and D145 will miss it. The fix is one row: record it as a payment for **`2026-07`**. Do not "fix" it by changing the counted-months rule.

## 10. Known limitations

- **Výměna elektroměru is out of scope (D150) — and there is no escape hatch, deliberately.** The guard is **global across all readings**, not per-period, so the first reading on a new meter (starting near 0) is refused with a 422 naming the old meter's last reading and cannot be entered at all; every interval spanning the swap would otherwise compute absurd consumption. **Do not work around it with a manual DB edit** — the audit trail would then not explain the jump, which is exactly what a future reader needs explained. The smallest honest fix, recorded so nobody re-derives it: add `meter_id` (or an `offset_dkwh`) to `electricity_readings`, scope the monotonicity guard and the interval walk to one meter, and make the walk **break** at a meter boundary rather than span it. Roughly a day including tests, plus a migration.
- **The prediction has no seasonality.** D141 is a plain average since the opening reading, so a period starting in June and first predicted in August will **under-forecast the heating season**; the mirror error happens in February. The mitigation is structural, not algorithmic — the average lengthens as readings are entered, and the Přehled always names the window it averaged over. Do not add a seasonal coefficient; it would be a second source of truth with no data behind it.
- Smaller, all deliberate: **no DPH arithmetic** (D135 — a VAT change is just a new ceník version, the D136 mechanism) · **Historie kWh columns are approximate** (D138; the Kč columns are exact totals cut along an approximate line, D159) · **the 30/70 headroom mix is a stated guess** · **one odběrné místo**, no FVE/přetoky/plyn/voda (a second would need `site_id` on four tables and a scope on every read) · **no invoice PDF**, so no blob storage · **no back-fill importer** — history is typed in as ordinary readings, and in Karel's case there is none.

## 11. Module packaging

```
backend/internal/modules/electricity/
    module.go              # registry.Module: routes, migrations, 15 audit actions — and NOTHING else
    http.go                # chi mount, 13 paths, role gates, the five inline date cursors
    service.go             # validation, WithTx + audit-in-tx, monotonicity + overlap + D160 guards
    store.go               # SQL incl. Snapshot(ctx, periodID)
    compute.go             # THE MODULE. pure: intervals, blocks, energy, fee chunks, forecast,
                           # counted months, balance, doporučená, headroom, history
    types.go · service_test.go · http_test.go
    compute_test.go        # the two fixtures + the property test   ← write with compute.go
    compute_purity_test.go # the import whitelist
    migrations/11001_electricity.sql     # the only migration. no seed/ directory.
frontend/src/modules/electricity/  —  pages/ api/ components/
```

Build `compute.go` first and pin it with the two fixtures before writing a line of SQL. Resist, in order: adding a widget, adding a metric "since it's nearly free", storing a computed total, interpolating kWh into a price, allocating a fee into an interval, and adding a fourth rounding point.
