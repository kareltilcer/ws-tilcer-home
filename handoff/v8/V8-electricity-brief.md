# Home v8 — **Elektřina** (`electricity`), design brief

> Status: **Resolved — scope frozen 2026-08-20 · full document round written 2026-08-20** · Owner: Karel
> All open questions answered (OQ-V8-1…10, plus OQ-V8-3a raised and closed during the interview).
> Folded into `PRD.md` as **§V8-1 … §V8-11**, `openapi.yaml` → **0.10.0**, `CHANGELOG.md`, `REGISTRY.md`,
> `HANDOFF-10-electricity.md` and a `HANDOFF-design.md` §v8 addendum.
> Decisions **D133–D160** (D157–D160 were forced back into the brief by the `HANDOFF-10` pass — see §7a).
> Migration block **11**. **Tenth** module (v7's `garden` is the ninth and is specified but not yet built).

---

## 0. Scope

**In:**

1. **Odečty** — meter readings taken at arbitrary dates: two registers, **VT** (vysoký tarif) and **NT** (nízký tarif).
2. **Ceník** — `cena za MWh ve VT`, `cena za MWh v NT`, `měsíční poplatky`; each version **effective from a date**, never touching anything before it.
3. **Zálohy** — a monthly amount with a due day, effective from a date, plus optional records of what was really paid in a given month.
4. **Zúčtovací období** — settlement periods you set yourself, with the supplier's real vyúčtování recorded when it arrives.
5. **Predikce** — cost so far + forecast to the end of the period, against the zálohy → **nedoplatek / přeplatek**, plus the **doporučená záloha** that would land the period at zero.
6. **Historie a grafy** — consumption and cost per month, VT vs NT.

**Out of v8, deliberately:**

- **No Nástěnka widget, no metrics, no lists, no push** (D147, D156) — Karel: *"no widget"*, *"no chase"*.
- **No invoice itemization** — no silová/distribuce split, no jistič, no systémové služby, POZE, OTE, daň z elektřiny, **no DPH arithmetic**: the three numbers are entered **s DPH, včetně distribuce** and used as typed (D135).
- **No výměna elektroměru** (D150), no second odběrné místo, no FVE/přetoky, no plyn or voda.
- No invoice PDF attachment (so: **no blob storage**), no HDO schedule, no price-offer comparison, no supplier switching workflow.
- No back-fill importer. Whatever history you want is typed in as ordinary readings — and in Karel's case there is none: the meter starts at 32 / 70 kWh on the period's first day.

---

## 1. How it sits in Home

`electricity` is the tenth module, registered like the others: own routes, own Goose migrations (block **11**), own audit actions. **No cross-module import** — `internal/arch`'s `TestModulesDoNotImportEachOther` stays green.

It is the **leanest module Home has** — the first that contributes **nothing** to Nástěnka and nothing to the notification catalogs:

| Platform package | Used for |
|---|---|
| `platform/db`, `audit`, `httpx`, `idgen`, `reqctx`, `dates` | the usual |
| `platform/metrics`, `platform/lists`, `platform/push`, `platform/scheduler`, `platform/blobstore` | **none of them** (D147, D152) |

That has one pleasant consequence and one trap worth stating.

**The consequence:** no `Source` interfaces to implement, no catalog keys to name, no widget provider, no `platform/widgets/registry.tsx` edit. Everything is computed **on read** — there is no stored derived value and no periodic job anywhere in the module (D152).

**The trap:** of the four **non-registry host maps** that v5, v6 and v7 all tripped over, **three** still have to be hand-edited or the module half-appears — and it is exactly the fourth one that must *not* be touched:

| Map | v8 |
|---|---|
| `AppShell`'s `OVERFLOW` nav list | **add** "Elektřina" → `/elektrina` |
| the Log browser's hardcoded `MODULES` | **add** `electricity` |
| `admin/listener.go`'s `inAppURL` | **add** `electricity` → `/elektrina` |
| `platform/widgets/registry.tsx` | **do not touch** — there is no widget |

---

## 2. The one idea the whole module rests on

**A reading is the state of the meter at 00:00 of its date (D134).**

Everything else falls out of that sentence:

- consumption of day *d* = `reading(d+1) − reading(d)`;
- an interval between readings on `d1` and `d2` covers the days `d1 … d2−1`;
- a ceník effective from `D` prices the days `D, D+1, …`;
- a settlement period `[starts_on, ends_on]` (inclusive, the way you'd write it: 24. 6. 2026 – 23. 6. 2027) needs an **opening reading dated `starts_on`** and is closed by the reading dated **`ends_on + 1`** — which is also the next period's opening reading. One meter reading, one instant, two periods. That is how the distributor does it too.

Without a single rule like this, every boundary in the module becomes an argument about whether a day is counted twice or not at all.

---

## 3. Data model

House conventions throughout: UUIDv7 ids, `created_by` / `created_at` / `updated_at`, **soft delete**, English ids and Czech UI, every mutation audited in the same transaction.

### 3.1 `electricity_readings` — odečty

| Column | Notes |
|---|---|
| `read_on` | DATE, **UNIQUE** among live rows — one reading per day |
| `vt_dkwh`, `nt_dkwh` | INTEGER, **tenths of kWh** (D148) |
| `note` | optional, e.g. "odečet kvůli změně ceny" |

**Karel's meter shows whole kWh (OQ-V8-1)**, so the form asks for and displays whole numbers. The storage stays in tenths anyway: it costs nothing today and saves a migration if a meter with a decimal ever appears.

**Validation:** both registers must be **non-decreasing in date order**, checked against the neighbours on *both* sides so a back-filled reading can't break the chain either. A reading that would make either register go down is refused with 422 and a Czech message naming the offending neighbour — because with výměna elektroměru out of scope (D150), a falling counter is always a typo.

### 3.2 `electricity_tariffs` — ceník

| Column | Notes |
|---|---|
| `effective_from` | DATE, **UNIQUE** |
| `price_vt_haler` | Kč/MWh **s DPH a distribucí**, stored in haléře |
| `price_nt_haler` | Kč/MWh **s DPH a distribucí**, stored in haléře |
| `monthly_fee_haler` | Kč/měsíc **s DPH**, stored in haléře |
| `note` | e.g. "ceník od 1. 1. 2027" |

**A version governs every day ≥ `effective_from` until the next version starts (D136).** No end date is stored — a stored end is a second source of truth that will eventually disagree with the next row's start. Editing a version's prices changes the numbers **only for its own days**; nothing before it moves. That is the "effective to some date, which will not affect numbers before that date" requirement, and it is structural rather than a rule someone has to remember.

### 3.3 `electricity_advances` — předpis záloh, and `electricity_payments` — skutečně zaplaceno

- `electricity_advances`: `{effective_from DATE UNIQUE, amount_haler, due_day INTEGER 1–31}` — versioned exactly like the ceník.
- `electricity_payments`: `{month TEXT 'YYYY-MM' UNIQUE, amount_haler, paid_on DATE NULL, note}` — optional, one row per month.

**A recorded payment wins over the schedule for its month (D144).** Months with no payment row use the schedule amount effective on the 1st of that month. So the common case is zero typing, and the month where you paid something different is one row.

**`due_day` (D155)** is clamped to the month's last day **at read time, never stored clamped**, the way v5's scheduled summaries already clamp day-of-month (§10 D74) — a due day of 31 falls on 28. února. It affects **only** "zálohy zaplaceno zatím" and therefore the *doporučená záloha*; how many months a period counts is D145's business and is unaffected. **At equality the month counts**: a záloha due on the 15th is "zaplaceno" on the 15th, not on the 16th.

### 3.4 `electricity_periods` — zúčtovací období

| Column | Notes |
|---|---|
| `starts_on` | DATE, inclusive |
| `ends_on` | DATE, inclusive — **an expected date until the supplier confirms it** (D153) |
| `ends_on_confirmed` | BOOL, default false — drives the "předpokládaný konec" badge |
| `invoiced_total_haler` | nullable — the total cost the supplier billed |
| `invoiced_balance_haler` | nullable — the nedoplatek (negative) / přeplatek (positive) they arrived at |
| `invoiced_vt_dkwh`, `invoiced_nt_dkwh` | nullable — the meter values the supplier billed **to** (D154) |
| `invoiced_at`, `note` | when the vyúčtování arrived |

Periods **must not overlap** (422). **Nothing is locked (D139).** A period stays editable forever; recording the invoice adds a comparison line — *"spočteno 21 560 Kč · vyúčtováno 21 890 Kč · rozdíl −330 Kč"* — and, because the final readings are stored too, a second line in **kWh**, which is how you catch an odhadnutý odečet on the supplier's side rather than a pricing surprise.

### 3.5 What there deliberately isn't

No settings table (nothing to configure), no derived/materialised table, no cache, no seed migration. `11001_electricity.sql` is the module's only migration.

---

## 4. The locked computation — `compute.go`

This is v8's `split.go`: **one pure function file, no DB access inside it, its own test file**, taking a loaded snapshot (readings, tariff versions, advances, payments, the period, `today`) and returning the whole summary. If the Přehled, the Odečty list and the Historie chart ever disagree about a number, the bug is unfindable — so they all read through this.

### 4.1 Money and units

Money in **haléře** (INTEGER), energy in **tenths of kWh** (INTEGER), no floats anywhere in the money path (D148).

For an interval with `vt`, `nt` in tenths of kWh, priced by one ceník version:

```
cost_haler = round( (vt · price_vt_haler + nt · price_nt_haler) / 10000 )
```

**One rounding per interval**, on the VT+NT sum — not per tariff, not per day.

### 4.2 Poplatky, pro-rata by days (D143)

A day's share of the monthly fee is `monthly_fee(day) / (days in that calendar month)`. Summed per **(calendar month × ceník version)** chunk, with **one rounding per chunk**:

```
chunk_haler = round( monthly_fee_haler · days_in_chunk / days_in_month )
```

A whole month inside one ceník version therefore costs exactly the monthly fee, to the haléř — the pro-rata only shows up at the ends of a period and at a mid-month price change.

### 4.3 The hard block (D137)

If a ceník `effective_from` falls **strictly inside** an interval — i.e. `d1 < effective_from < d2` — that interval **is not priced at all**. The module reports `chybí odečet k <date>` and computes nothing from that date onward; everything **before** the gap stays valid and visible.

The same applies to a period whose **opening reading is missing** (D140, confirmed at OQ-V8-4): with no reading on `starts_on` there is no baseline, so the period shows no money at all, only the missing-reading notice.

**Nothing is ever interpolated for money.** The counterpart rule: the Historie chart *does* spread an interval's kWh evenly across its days so it can draw month columns (D138) — that is display only, it is labelled as approximate, and it never feeds a Kč figure.

**Which raises the obvious question the chart asks: what is "cost per month"? (D159)** Not a repricing of interpolated kWh — that would be exactly the thing D137 forbids. Instead the month's Kč column is an **allocation of already-exact interval costs by day count**: an interval's cost is computed once, from real meter deltas, and then divided across the months it spans in proportion to days. Fee chunks are already per month and need no allocation. So the kWh columns are approximate and say so, the Kč columns are exact totals cut along an approximate line, and no price ever touches an invented meter value.

### 4.4 Prediction — plain average since period start (D141)

The boundary between *fact* and *forecast* is the **latest reading**, not today. Days since the last reading are forecast like any other future day.

```
elapsed_days = last_reading.read_on − period.starts_on          (must be ≥ 1)
avg_vt_per_day = (vt_last − vt_start) / elapsed_days
avg_nt_per_day = (nt_last − nt_start) / elapsed_days
```

For every day *d* from `last_reading.read_on` to `ends_on` inclusive, the forecast uses the ceník **effective on that day** (D142) — so a price rise you have already entered with a future date is honoured automatically, and you can see the effect of a new ceník the moment you type it.

```
cost_total    = cost_actual (period start → last reading) + cost_forecast (last reading → period end)
advances      = Σ over counted months of (payment if recorded, else scheduled amount)
balance       = advances − cost_total          > 0 přeplatek · < 0 nedoplatek
recommended   = (cost_total − advances for months already due) / months not yet due
```

**Which months count (D145):** a calendar month belongs to the period **iff the period contains that month's first day**. A year-long period is therefore always exactly 12 months, whatever day of the month it starts on — Karel's 24. 6. 2026 – 23. 6. 2027 counts červenec 2026 … červen 2027, checked. The Přehled lists the counted months with their amounts, so the rule is visible rather than folklore.

**Which months are already paid (D155):** a counted month is "zaplaceno" once its **due day has passed**, not the moment the month starts. This is the only place `due_day` is read, and it moves only the *doporučená záloha*, never the period total.

**When the period is over (D157):** once a reading dated **`ends_on + 1`** exists — the closing reading, which is also the next period's opening one — the forecast span is empty and the period is **entirely actual**. That is the state in which the computed-vs-invoiced comparison (D154) becomes meaningful, and the Přehled swaps "predikce" for "skutečnost". Without this case the module would keep forecasting a period it already knows the answer to.

**When prediction is impossible:** fewer than two readings in the period, `elapsed_days < 1`, no ceník version effective on or before `starts_on`, or an unresolved hard block — the Přehled says *"zatím nelze předpovědět"* and names what's missing. It never shows a number it hasn't earned.

**Showing VT and NT separately (D158).** D148 rounds once, on the VT+NT sum — so a displayed per-tariff breakdown cannot round independently or the two parts will occasionally miss the headline by a haléř. **VT rounds, NT takes the remainder.** This is the `needs` pattern from the fin split ([[fin-budget-calc]]): one component is derived by subtraction and absorbs the rounding error, so the parts always sum to the whole by construction rather than by luck.

### 4.5 Worked example — the general case

Period **1. 4. 2026 – 31. 3. 2027**. Ceník A od 1. 1. 2026: VT **3 200**, NT **2 400** Kč/MWh, poplatky **350** Kč/měs. Ceník B od 1. 1. 2027: VT **3 600**, NT **2 700**, poplatky **380**. Záloha **1 800** Kč/měs, **splatnost 15.** Today = 20. 8. 2026.

| Odečet | VT | NT |
|---|---|---|
| 1. 4. 2026 | 12 000 | 30 000 |
| 1. 8. 2026 | 12 640 | 31 480 |

- Actual: 122 days, VT 640 kWh, NT 1 480 kWh → `640·3,20 + 1480·2,40` = **5 600 Kč** + 4 whole months × 350 = **1 400 Kč** → **7 000 Kč**.
- Averages: VT **5,2459** kWh/den, NT **12,1311** kWh/den.
- Forecast 1. 8. 2026 → 31. 3. 2027 = 243 days: **153** on ceník A, **90** on ceník B.
  - energy: 2 568,39 + 4 454,56 (A) + 1 699,67 + 2 947,87 (B) = **11 670,49 Kč**
  - poplatky: 5 × 350 + 3 × 380 = **2 890 Kč**
- **Celkem 21 560 Kč** · zálohy 12 × 1 800 = **21 600 Kč** → **přeplatek 40 Kč**.
- Doporučená záloha: five zálohy are due by 20. 8. (duben…srpen, splatnost 15.), so (21 560,49 − 5 × 1 800) / 7 = **1 795 Kč/měs** (rounded up). **The due day is load-bearing here** — at splatnost 25. only four are due and the figure becomes 1 796 Kč. Any test of this example must pin it.

Note what the example demonstrates: the January price rise is already priced into an August prediction, and no reading was needed on 1. 1. 2027 *yet* — the hard block only bites once an interval actually straddles the change. *(Arithmetic re-verified 2026-08-20. If a later change disagrees with these numbers, the change is wrong.)*

### 4.6 Karel's actual starting state (OQ-V8-3)

This is what the module contains on day one, and the first fixture the implementation should be tested against.

| | |
|---|---|
| **Ceník** (od 24. 6. 2026) | VT **4 858,65** Kč/MWh · NT **4 026,69** Kč/MWh · poplatky **642,35** Kč/měs — all **s DPH a distribucí** |
| **Záloha** | **1 500** Kč/měs (due day per OQ-V8-10, to be filled in) |
| **Období** | **24. 6. 2026 – 23. 6. 2027**, end date **předpokládaný** (D153) — the supplier hasn't stated it |
| **Počáteční odečet** | 24. 6. 2026 — VT **32** kWh, NT **70** kWh (new meter) |
| **Další odečet** | none yet |

Three things follow immediately, and each is an acceptance case:

1. **The period counts exactly 12 zálohy** — červenec 2026 … červen 2027 — even though it starts on the 24th. D145 verified against these dates.
2. **No prediction is possible yet.** One reading in the period ⇒ Přehled says *"zatím nelze předpovědět — potřebuji druhý odečet"*, and shows the ceník, the období and the zálohy predpis, nothing else. This empty state is the **first thing Karel will ever see**, so it has to be a designed screen, not a blank page with a spinner.
3. **The headroom is worth stating on screen.** Of the 1 500 Kč záloha, **642,35 Kč is poplatky**, leaving **857,65 Kč/měs for energy** ≈ **176 kWh** if all VT, **213 kWh** if all NT, **~200 kWh** at a 30/70 mix. That figure is computable before any consumption is known, it's the honest answer to "will the zálohy cover it", and it costs nothing to show while the real prediction is still unavailable.

⚠ **One thing to check when the first záloha is paid:** červen 2026 does *not* count toward the period (the period does not contain 1. 6.). If a záloha was actually paid in June for this supply, it belongs to the period and D145 will miss it — record it as a payment for `2026-07`, or the period's first month needs revisiting.

---

## 5. API

Paths under `/api/electricity/`, house wire format (snake_case, `PATCH`, `Limit`/`Cursor`).

| Method | Path | Notes |
|---|---|---|
| `GET/POST` | `/readings` | list newest-first; `POST` validates monotonicity |
| `PATCH/DELETE` | `/readings/{id}` | soft delete |
| `GET/POST` | `/tariffs` | ceník versions, newest-first |
| `PATCH/DELETE` | `/tariffs/{id}` | **409 only if the deletion would leave a day inside a settlement period with no effective ceník** (D160) — i.e. the earliest version covering a period's start. Deleting a middle version legitimately reprices its days, and the audit event records it |
| `GET/POST` | `/advances`, `PATCH/DELETE` `/advances/{id}` | the záloha schedule — a mistyped amount or due day must be fixable |
| `GET/POST` | `/payments`, `PATCH/DELETE` `/payments/{id}` | real payments, one row per `YYYY-MM` |
| `GET/POST` | `/periods`, `PATCH/DELETE` `/periods/{id}` | overlap refused (422) |
| `GET` | `/summary?period_id=` | the whole computed picture: actual, forecast, balance, recommended záloha, counted months, headroom, and `blocking` |
| `GET` | `/history?from=&to=` | per-month consumption + cost series for the charts; `from`/`to` are **`YYYY-MM`** |
| `GET` | `/intervals?period_id=` | each reading-to-reading interval with kWh, **energy Kč** and which ceník priced it — the "show your work" view behind the Odečty list |

⚠ **Cursors are dates, not UUIDv7 (D149)** — `/readings` keysets on `read_on`, `/tariffs` and `/advances` on `effective_from`, because the user-visible order is chronological and ordering by id would misplace a back-filled row. This is the **finance month-key precedent** (see `home-service` memory, §"the one deliberate spec-vs-build difference"). A malformed cursor **422s** rather than silently re-serving page one, and these parameters are declared **inline** — they must not `$ref` the shared UUIDv7 `Cursor`.

⚠ **openapi.yaml editing rule inherited from v7:** any inline `{ … }` flow-mapping `description:` containing a comma **must be quoted**, or YAML eats the tail as a stray null key.

---

## 6. Screens

Four routes under `/elektrina`, reached from the **"více"** overflow. Ordinary all-roles module: `reader` reads, `editor`/`admin` write, **no admin-only route**.

**Přehled** — the current period, and the only screen that answers the actual question:

- the headline: **nedoplatek / přeplatek** at the period end, with the date and, while `ends_on` is unconfirmed, the words **předpokládaný konec** next to it — the prediction always names the date it projected to (D153);
- under it, *"predikce z průměru za posledních 122 dní"*, so the number is never mistaken for a fact;
- a progress line: days elapsed / days remaining;
- **spotřeba a náklady k dnešku, VT a NT zvlášť** (D151) — kWh, Kč and each tariff's share of the total. With NT at 4 027 vs VT at 4 859 Kč/MWh, roughly 830 Kč rides on every MWh moved from VT to NT; the split is the only way to see whether the NT hours are being used;
- **zálohy** — paid so far (by due day, D155) / expected total, with the counted months on demand;
- **doporučená záloha** vs. the current one;
- a plain line: *"poslední odečet před 47 dny"* with a **Zadat odečet** button (D156). Not a notification — nothing is sent anywhere, nothing appears on Nástěnka;
- when prediction isn't possible: the **headroom** line from §4.6 — what the current záloha buys in kWh per month — instead of an empty panel;
- when blocked: one prominent Czech line — *"Chybí odečet k 1. 1. 2027 (změna ceny). Bez něj nelze spočítat spotřebu po tomto datu."* with a button that opens the reading form **pre-filled with that date** (the date only — never the values, D137).

**Odečty** — the list of readings, each row showing the interval that ends at it: days, VT/NT kWh, **Kč za energii**, and which ceník priced it. Add / edit / delete, whole kWh in the input. This is where a mistyped register becomes obvious, because one interval will look absurd next to its neighbours.

The Kč on a row is **energy only, and labelled so**. Poplatky are chunked by (month × ceník version) per D143 and do not belong to any one interval; allocating them into intervals would invent a second rounding rule to keep two views agreeing, for no gain. The period total is stated as *energie + poplatky* on Přehled, which is also how the supplier states it.

**Ceníky a poplatky** — the versions in date order, each with its three numbers, its effective-from date, its derived validity range and *"platí pro 153 dní tohoto období"*. Adding a version with a future date is the normal case, not an edge case. The záloha schedule and its due day live here too.

**Historie a grafy** — consumption per month (VT/NT), cost per month, and past periods with computed vs. invoiced in both **Kč and kWh** (D154). Charts follow the **Path A palette** decision (`home-design-palette`): VT and NT are two series that must also differ by **more than colour** — pattern or direct labels — and the month columns carry the "interpolated, approximate" note from D138. Light theme checked first, 375 px checked first.

---

## 7. Decisions D133–D156

| # | Decision |
|---|---|
| **D133** | `electricity` — tenth module, Czech UI **Elektřina**, route `/elektrina`, nav in "více". Migration block **11**, OpenAPI **0.10.0**, `HANDOFF-10-electricity.md`. *(OQ-V8-9)* |
| **D134** | **A reading is the meter state at 00:00 of `read_on`.** Consumption of day *d* = `reading(d+1) − reading(d)`. Every interval, ceník and period boundary is expressed this way. |
| **D135** | The ceník is **three numbers only** — `cena VT` a `cena NT` (Kč/MWh) a `měsíční poplatky` (Kč/měs) — **all including DPH and distribuce**, used as typed. No itemization, no VAT rate, no jistič. |
| **D136** | A ceník version governs **all days ≥ `effective_from`**; its end is derived from the next version, never stored. Editing a version moves only its own days. |
| **D137** | **Hard block.** A ceník change strictly inside a reading interval makes that interval unpriceable; the module names the missing date and computes nothing after it. Days before the gap stay valid. **Money is never interpolated.** |
| **D138** | The **history chart** *does* spread an interval's kWh evenly over its days, so months can be drawn. Display only, labelled approximate, never feeds a Kč figure. The two halves of this pair must be read together. |
| **D139** | Settlement periods are **user-set, inclusive `[starts_on, ends_on]`, non-overlapping, never locked**. The real vyúčtování is recorded as optional fields and produces a computed-vs-invoiced line. |
| **D140** | A period **requires a reading on `starts_on`**. Without it the period has no baseline and shows no money at all — the direct consequence of D137. *(Confirmed, OQ-V8-4.)* |
| **D141** | Prediction = **plain average since period start**, VT and NT separately, measured from the opening reading to the **latest reading**. Days after the last reading are forecast, not actual. |
| **D142** | The forecast prices each future day with the ceník **effective on that day**, so an already-entered future price change is honoured. |
| **D143** | **Poplatky pro-rata by days**: a day costs `monthly_fee(day) / days_in_month`, summed per (month × ceník version) chunk with one rounding. |
| **D144** | Zálohy = **schedule + optional real payments**. `{effective_from, amount, due_day}` versioned like the ceník; a `{month, amount}` payment row wins for its month. |
| **D145** | A calendar month counts toward a period **iff the period contains that month's first day**. A year-long period is always exactly 12 months. *(Confirmed, OQ-V8-2; verified against 24. 6. 2026 – 23. 6. 2027.)* |
| **D146** | `balance = zálohy − cost_total`; **> 0 přeplatek, < 0 nedoplatek**. `recommended = (cost_total − zálohy for months already due) / months not yet due`, **rounded up to whole Kč**, floored at 0; not shown when no months remain. |
| **D147** | **No widget, no metrics, no lists, no push.** First Home module contributing nothing to Nástěnka or the notification catalogs. Exactly **three** of the four non-registry host maps are edited; `platform/widgets/registry.tsx` is **not** one of them. |
| **D148** | Energy stored as INTEGER **tenths of kWh**, money as INTEGER **haléře**. No floats in the money path. One rounding per interval, one per fee chunk. All of it in a **pure `compute.go`** with its own tests — the module's `split.go`. **The form takes whole kWh** — Karel's meter has no decimal (OQ-V8-1) — but the storage keeps the tenth so a future meter needs no migration. |
| **D149** | Keyset cursors are **dates** (`YYYY-MM-DD`), declared inline, 422 on malformed — the finance month-key precedent. Never `$ref` the shared UUIDv7 `Cursor`. |
| **D150** | **Výměna elektroměru is out of scope.** The schema assumes one monotonically non-decreasing pair of counters and refuses a reading that would decrease either register. Recorded as a known limitation, not an oversight. |
| **D151** | Ordinary all-roles module: `reader` reads, `editor`/`admin` write, soft delete, **no admin-only route**. Four screens: Přehled · Odečty · Ceníky a poplatky · Historie. **Přehled breaks consumption and cost down VT vs NT** with each tariff's share *(OQ-V8-7)*. |
| **D152** | **Everything is computed on read.** No derived column, no cache table, no scheduler job, no seed migration. `11001_electricity.sql` is the module's only migration. |
| **D153** | **`ends_on` is an expected date until confirmed.** Defaults to `starts_on + 1 year − 1 day`, editable, carries `ends_on_confirmed`; while false the UI says **předpokládaný konec** and the prediction always names the date it projected to. Correcting it later is one field, and every number follows. *(OQ-V8-3a — raised because Karel's supplier hasn't stated an end date.)* |
| **D154** | The vyúčtování record stores **four** figures: invoiced total, invoiced balance, and the supplier's final **VT and NT readings** — so a discrepancy is attributable to **kWh** rather than only to Kč, which is how an odhadnutý odečet on their side becomes visible. Still no locking. *(OQ-V8-5.)* |
| **D155** | The záloha schedule carries **`due_day`** (1–31, clamped to the month's last day — the §10 D74 precedent). It is read in exactly one place: whether a counted month is already "zaplaceno". It therefore moves only the *doporučená záloha*, never the period total, and **D145's counting is unaffected**. *(OQ-V8-10.)* |
| **D156** | **"No chase" means no push, no metrics, no lists, no widget — and one plain in-app line.** Přehled shows *"poslední odečet před N dny"* with a **Zadat odečet** button. Text on a page you already opened is not a notification; it is also the honest explanation of why a prediction is stale. *(OQ-V8-6.)* |

### 7a. The four corrections the handoff pass forced back (D157–D160)

Writing `HANDOFF-10-electricity.md` against this brief surfaced four places where the spec was silent or self-contradictory. Recorded here rather than fixed quietly, the way v7 recorded the two corrections its handoff forced:

| # | Decision |
|---|---|
| **D157** | **A period with its closing reading is entirely actual.** Once a reading dated `ends_on + 1` exists the forecast span is empty, Přehled says *skutečnost* instead of *predikce*, and the computed-vs-invoiced comparison (D154) becomes meaningful. §4.4 previously described only the mid-period case, so a finished period would have gone on forecasting an answer it already had. |
| **D158** | **A displayed VT/NT cost split rounds VT and gives NT the remainder.** D148 rounds once on the VT+NT sum; two independent roundings would occasionally miss the headline by a haléř. Same shape as `needs` in the fin split — one component absorbs the error by subtraction. |
| **D159** | **"Cost per month" on Historie is an allocation of exact interval costs by day count**, never a repricing of interpolated kWh. Without this, §6's month-cost chart and D137's "money is never interpolated" contradict each other outright. |
| **D160** | **A ceník version may be deleted unless the deletion would leave a day inside a settlement period with no effective version** (409). The earlier "covering existing days" wording would have frozen every version ever used; deleting a middle version legitimately reprices its days and is audited like any other change. |

Two smaller gaps closed in place: **D155 counts a month at equality** (due on the 15th ⇒ paid on the 15th), and the §4.5 worked example now pins **splatnost 15.**, because the doporučená záloha in it is 1 795 Kč at splatnost ≤ 20 and 1 796 Kč at splatnost 25 — a fixture that doesn't state the due day isn't reproducible.

---

## 8. Questions asked and answered (2026-08-20)

| # | Question | Answer |
|---|---|---|
| OQ-V8-1 | Decimal on the VT/NT registers? | **Whole kWh only** → form whole, storage tenths (D148) |
| OQ-V8-2 | How many zálohy belong to a period? | **Month counts if the period contains its 1st** (D145) |
| OQ-V8-3 | Real numbers | 4 858,65 / 4 026,69 Kč/MWh · 642,35 Kč/měs · záloha 1 500 · období od 24. 6. 2026 · odečet 32/70 — §4.6 |
| OQ-V8-3a | Period end unknown — project to what? | **Expected end, editable** (D153) |
| OQ-V8-4 | Require a reading on the period start date? | **Confirmed — require it** (D140) |
| OQ-V8-5 | What to store from the vyúčtování? | **Total + balance + the supplier's final readings** (D154) |
| OQ-V8-6 | Does "no chase" ban an in-app line? | **The line is fine** (D156) |
| OQ-V8-7 | VT/NT breakdown on Přehled? | **Break it down** (D151) |
| OQ-V8-8 | Historical readings to back-fill? | **None** — the meter starts at 32/70 on the period's first day |
| OQ-V8-9 | Naming and route | **Elektřina · `/elektrina` · `electricity`** (D133) |
| OQ-V8-10 | Due day for "zaplaceno zatím"? | **Record a due day** (D155) |

Two items are still blank and are the implementer's first questions back: the **záloha's due day** (a number 1–31), and the supplier's real **end date** for the period once it is known.

---

## 9. Acceptance criteria (draft, to be finalised in the PRD)

1. A ceník version added with a future date changes **no** past or present figure, and immediately changes the forecast for days on and after its date.
2. Editing a ceník version's prices changes only the days that version governs — a regression test asserts the previous period's total is byte-identical.
3. An interval straddling a ceník change is **refused, not estimated**; the summary reports `chybí odečet k <date>`; adding that reading resolves it and the numbers before the gap never moved.
4. The period **24. 6. 2026 – 23. 6. 2027** counts **12** months of zálohy — červenec 2026 … červen 2027 — and červen 2026 is not among them.
5. A whole calendar month inside one ceník version is charged **exactly** the monthly fee — no haléř lost to pro-rata rounding.
6. A reading that would make either register decrease is refused with a Czech message naming the neighbouring reading.
7. The Přehled's **energy** total equals the sum of the per-interval energy costs shown on Odečty, to the haléř, and its **poplatky** total equals the sum of the (month × ceník) fee chunks — for a fixture with two ceník versions and a partial month at each end. The displayed VT + NT parts sum exactly to the energy total (D158).
8. **Karel's real opening state** — one reading, no second — renders the designed empty state: *"zatím nelze předpovědět"*, the ceník, the období with **předpokládaný konec**, and the **857,65 Kč / ~200 kWh headroom line**. No spinner, no blank panel, no zero.
9. A `due_day` of 31 marks únor's záloha due on the 28th (29th in a leap year), and changing `due_day` moves the *doporučená záloha* but leaves the period total and the counted months identical.
10. Changing an unconfirmed `ends_on` re-projects the whole forecast and changes nothing about the actual (pre-last-reading) figures.
11. Entering the closing reading (`ends_on + 1`) empties the forecast, flips Přehled from *predikce* to *skutečnost*, and enables the computed-vs-invoiced comparison (D157).
12. `compute.go` has no `database/sql` import (asserted by a test), and `internal/arch` stays green.
13. `electricity` registers **no** widget, metric or list — asserted, so a future refactor can't quietly add one.
