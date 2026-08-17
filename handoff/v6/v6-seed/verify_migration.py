#!/usr/bin/env python3
"""
The retirement gate (PRD D97 / §V6-12 step 4).

    python3 verify_migration.py fin-months-export.json home-finance-export.json

Compares `fin`'s live months against home's, row for row and field for field,
INCLUDING all nine computed split values. Exit 0 = clean, exit 1 = a mismatch.

**Do not stop `fin` until this exits 0.** With no redirect standing behind the
cutover (D96) there is no post-hoc fallback, and comparing inputs alone would
not catch the one mistake this migration can actually make — a formula
transcribed wrong. That is precisely what the split comparison is for.

Getting the two inputs:

  fin   POST https://fin.tilcer.cz/login  -> access_token
        GET  https://fin.tilcer.cz/months  (Authorization: Bearer …)   [unpaged]
  home  GET  https://home.tilcer.cz/api/finance/months?limit=200       (session cookie)
        -> save the `items` array, or the whole page object; both are accepted.
"""
import json
import sys

# fin is camelCase; home is snake_case (PRD D92). The field mapping IS the contract
# between the two services, so it is spelled out rather than derived by regex — a
# silent mis-derivation here would make the gate pass on a comparison it never made.
INPUTS = {"incomeKaja": "income_kaja", "incomeAndy": "income_andy"}
RATES = {"personal": "personal", "operational": "operational", "fun": "fun", "noFun": "no_fun"}
SPLIT = {
    "totalIncome": "total_income", "personalKaja": "personal_kaja",
    "personalAndy": "personal_andy", "toOperationalKaja": "to_operational_kaja",
    "toOperationalAndy": "to_operational_andy", "operationalReceived": "operational_received",
    "funSavings": "fun_savings", "noFunSavings": "no_fun_savings", "needs": "needs",
}


def load(path):
    d = json.load(open(path, encoding="utf-8"))
    return d["items"] if isinstance(d, dict) and "items" in d else d


def main():
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    fin, home = load(sys.argv[1]), load(sys.argv[2])
    fin_by, home_by = {m["id"]: m for m in fin}, {m["id"]: m for m in home}
    problems = []

    if len(fin) != len(home):
        problems.append(f"row count: fin {len(fin)} vs home {len(home)}")
    for missing in sorted(set(fin_by) - set(home_by)):
        problems.append(f"{fin_by[missing]['month']} ({missing}): in fin, MISSING from home")
    for extra in sorted(set(home_by) - set(fin_by)):
        problems.append(f"{home_by[extra]['month']} ({extra}): in home, not in fin")

    for mid in sorted(set(fin_by) & set(home_by), key=lambda i: fin_by[i]["month"]):
        f, h = fin_by[mid], home_by[mid]
        w = f["month"]
        if f["month"] != h["month"]:
            problems.append(f"{w}: month {f['month']} vs {h['month']}")
        for a, b in INPUTS.items():
            if f[a] != h.get(b):
                problems.append(f"{w}: {a} fin={f[a]} home={h.get(b)}")
        for a, b in RATES.items():
            if f["rates"][a] != (h.get("rates") or {}).get(b):
                problems.append(f"{w}: rates.{a} fin={f['rates'][a]} home={(h.get('rates') or {}).get(b)}")
        for a, b in SPLIT.items():
            if f["split"][a] != (h.get("split") or {}).get(b):
                problems.append(f"{w}: SPLIT.{a} fin={f['split'][a]} home={(h.get('split') or {}).get(b)}")
        if f["createdAt"] != h.get("created_at"):
            problems.append(f"{w}: created_at fin={f['createdAt']} home={h.get('created_at')}")

    if problems:
        print(f"VERIFICATION FAILED — {len(problems)} problem(s). DO NOT RETIRE fin.", file=sys.stderr)
        for p in problems:
            print("  -", p, file=sys.stderr)
        return 1
    print(f"VERIFIED: {len(fin)} months identical across inputs, rates, all nine split "
          f"values, and created_at. The retirement gate is open.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
