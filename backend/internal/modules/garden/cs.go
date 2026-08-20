package garden

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
)

// CZECH COPY, SERVER SIDE.
//
// The module's most-read strings are its warnings, and they are written HERE
// rather than on the frontend for one reason: the same sentence has to come out
// of the check panel, the audit log's summary, and a push notification composed
// in Administrace. A second implementation in TypeScript would drift, and the
// first person to notice would be reading two different explanations of the same
// warning on two screens.
//
// Czech has three plural forms — 1 / 2–4 / 5+ — and getting them wrong is the
// most visible possible sloppiness in a Czech-only UI. One helper, used
// everywhere.

// plural picks the Czech form for n: one / few (2–4) / many (5+, and 0).
func plural(n int, one, few, many string) string {
	switch {
	case n == 1:
		return one
	case n >= 2 && n <= 4:
		return few
	default:
		return many
	}
}

func countCS(n int, one, few, many string) string {
	return strconv.Itoa(n) + " " + plural(n, one, few, many)
}

func yearsCS(n int) string  { return countCS(n, "rok", "roky", "let") }
func daysCS(n int) string   { return countCS(n, "den", "dny", "dní") }
func tasksCS(n int) string  { return countCS(n, "práce", "práce", "prací") }
func bedsCS(n int) string   { return countCS(n, "záhon", "záhony", "záhonů") }
func warningsCS(n int) string { return countCS(n, "varování", "varování", "varování") }

// yearsAgoCS renders a gap in seasons the way a person says it. "Před 1 lety" is
// the kind of phrasing that makes an app feel machine-written, and this string
// sits in the module's single most important warning.
func yearsAgoCS(gap int) string {
	switch gap {
	case 0:
		return "letos"
	case 1:
		return "loni"
	case 2:
		return "předloni"
	default:
		return "před " + yearsCS(gap)
	}
}

// czDate renders a date as `d. M. yyyy` (PRD §5 conventions).
func czDate(d dates.Date) string {
	return fmt.Sprintf("%d. %d. %d", d.D, int(d.M), d.Y)
}

// czDateShort drops the year, for ranges inside one season where the year is
// already established by the page.
func czDateShort(d dates.Date) string {
	return fmt.Sprintf("%d. %d.", d.D, int(d.M))
}

// czRange writes a window as a RANGE, never as a single date — "vysaď rajčata
// mezi 15. a 25. květnem" is the truth, and a calendar that draws it as a point
// is lying about how gardening works.
func czRange(from, to dates.Date) string {
	switch {
	case from.Equal(to):
		return czDate(from)
	case from.Y == to.Y && from.M == to.M:
		// 15.–25. 5. 2027
		return fmt.Sprintf("%d.–%d. %d. %d", from.D, to.D, int(to.M), to.Y)
	case from.Y == to.Y:
		return fmt.Sprintf("%s – %s", czDateShort(from), czDate(to))
	default:
		return fmt.Sprintf("%s – %s", czDate(from), czDate(to))
	}
}

// weekLabelCS turns an internal "2027-W12" bucket key into "12/2027".
func weekLabelCS(key string) string {
	parts := strings.SplitN(key, "-W", 2)
	if len(parts) != 2 {
		return key
	}
	return strings.TrimLeft(parts[1], "0") + "/" + parts[0]
}

// monthsCS are the Czech month names in the genitive, which is the case a date
// takes in running text ("15. května").
var monthsCS = [...]string{
	"", "ledna", "února", "března", "dubna", "května", "června",
	"července", "srpna", "září", "října", "listopadu", "prosince",
}

// czDateWords renders "15. května 2027", for sentences rather than tables.
func czDateWords(d dates.Date) string {
	if int(d.M) < 1 || int(d.M) > 12 {
		return czDate(d)
	}
	return fmt.Sprintf("%d. %s %d", d.D, monthsCS[int(d.M)], d.Y)
}

// taskTitleCS builds a generated task's Czech title from its kind and the crop
// and bed it belongs to: "Výsev do sadbovačů — rajče Black Krim", "Výsadba —
// rajče (A1)". Built here so the calendar, the widget, the print sheet and the
// audit summary all say the same thing.
func taskTitleCS(kind string, e Effective, bedCode string) string {
	var b strings.Builder
	b.WriteString(taskKindTitleCS(kind))
	b.WriteString(" — ")
	b.WriteString(e.PlantName)
	if e.VarietyName != nil && *e.VarietyName != "" {
		b.WriteString(" ")
		b.WriteString(*e.VarietyName)
	}
	if bedCode != "" {
		b.WriteString(" (")
		b.WriteString(bedCode)
		b.WriteString(")")
	}
	return b.String()
}

// taskKindTitleCS is the capitalised, sentence-leading form of a task kind. The
// enum registry's labels are lowercase because they render in pickers; a title
// needs the other case, and duplicating the words here would be the usual way
// two spellings of "pikýrování" end up in one app — so it reads from the
// registry and only fixes the case.
func taskKindTitleCS(kind string) string {
	label := Label(EnumTaskKind, kind)
	if label == "" {
		return kind
	}
	r := []rune(label)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// driftMessageCS states, in Czech, that reality has moved and the plan has not
// (D119). It is built in the service rather than the frontend so the planting
// page, the widget and any future summary say it identically.
func driftMessageCS(stage string, days int) string {
	if days == 0 {
		return ""
	}
	verb := map[string]string{
		"sow":        "vyseto",
		"transplant": "vysazeno",
		"harvest":    "sklizeno",
	}[stage]
	if verb == "" {
		verb = "provedeno"
	}
	if days > 0 {
		return fmt.Sprintf("%s o %s později, sklizeň v plánu beze změny", verb, daysCS(days))
	}
	return fmt.Sprintf("%s o %s dřív, sklizeň v plánu beze změny", verb, daysCS(-days))
}
