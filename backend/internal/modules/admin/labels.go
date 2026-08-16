package admin

// Human Czech phrases for audit action keys (HANDOFF-design §v5, hard problem 2).
//
// An admin picking what to be notified about must read "Když někdo dokončí
// připomínku", never `reminder.complete`. The raw key stays visible as quiet
// secondary text in the composer — honest about what actually fires — but it is
// never the primary label.
//
// The map is keyed by "module.action" because the same bare verb can mean
// different things in different modules (notes' `folder.create` vs documents'
// `document_folder.create`), and because that is exactly how the composer groups
// them.
var actionLabels = map[string]string{
	// todo (Úkoly)
	"todo.board.create":  "Když někdo vytvoří nástěnku",
	"todo.board.update":  "Když někdo přejmenuje nástěnku",
	"todo.board.delete":  "Když někdo smaže nástěnku",
	"todo.column.create": "Když někdo přidá sloupec",
	"todo.column.update": "Když někdo upraví sloupec",
	"todo.column.move":   "Když někdo přesune sloupec",
	"todo.column.delete": "Když někdo smaže sloupec",
	"todo.card.create":   "Když někdo přidá úkol",
	"todo.card.update":   "Když někdo upraví úkol",
	"todo.card.move":     "Když někdo přesune úkol (třeba do Hotovo)",
	"todo.card.delete":   "Když někdo smaže úkol",
	"todo.label.create":  "Když někdo vytvoří štítek",
	"todo.label.update":  "Když někdo upraví štítek",
	"todo.label.delete":  "Když někdo smaže štítek",

	// events (Okno do budoucnosti)
	"events.event.create":        "Když někdo přidá událost",
	"events.event.update":        "Když někdo upraví událost",
	"events.event.delete":        "Když někdo smaže událost",
	"events.reminder.complete":   "Když někdo dokončí připomínku",
	"events.reminder.uncomplete": "Když někdo vrátí dokončenou připomínku",

	// notes (Poznámky)
	"notes.note.create":   "Když někdo vytvoří poznámku",
	"notes.note.update":   "Když někdo upraví poznámku",
	"notes.note.move":     "Když někdo přesune poznámku",
	"notes.note.delete":   "Když někdo smaže poznámku",
	"notes.note.pin":      "Když někdo připne poznámku pro všechny",
	"notes.note.unpin":    "Když někdo odepne poznámku",
	"notes.folder.create": "Když někdo vytvoří složku poznámek",
	"notes.folder.update": "Když někdo přejmenuje složku poznámek",
	"notes.folder.move":   "Když někdo přesune složku poznámek",
	"notes.folder.delete": "Když někdo smaže složku poznámek",

	// documents (Dokumenty)
	"documents.document.create":        "Když někdo nahraje dokument",
	"documents.document.update":        "Když někdo přejmenuje dokument",
	"documents.document.move":          "Když někdo přesune dokument",
	"documents.document.delete":        "Když někdo smaže dokument",
	"documents.document.pin":           "Když někdo připne dokument pro všechny",
	"documents.document.unpin":         "Když někdo odepne dokument",
	"documents.document_folder.create": "Když někdo vytvoří složku dokumentů",
	"documents.document_folder.update": "Když někdo přejmenuje složku dokumentů",
	"documents.document_folder.move":   "Když někdo přesune složku dokumentů",
	"documents.document_folder.delete": "Když někdo smaže složku dokumentů",

	// platform
	"platform.login":            "Když se někdo přihlásí",
	"platform.logout":           "Když se někdo odhlásí",
	"platform.push.subscribe":   "Když si někdo zapne oznámení na zařízení",
	"platform.push.unsubscribe": "Když si někdo vypne oznámení na zařízení",
	"platform.push.prefs":       "Když si někdo změní nastavení oznámení",
	"platform.push.test":        "Když si někdo pošle zkušební oznámení",

	// logging
	"logging.prune": "Když se pročistí historie logu",

	// admin — listed for completeness; the listener deliberately ignores its own
	// module, so a rule bound to these never fires (see listener.OnEvent).
	"admin.broadcast.send":    "Když správce rozešle oznámení",
	"admin.rule.create":       "Když správce vytvoří pravidlo",
	"admin.rule.update":       "Když správce upraví pravidlo",
	"admin.rule.delete":       "Když správce smaže pravidlo",
	"admin.schedule.create":   "Když správce vytvoří souhrn",
	"admin.schedule.update":   "Když správce upraví souhrn",
	"admin.schedule.delete":   "Když správce smaže souhrn",
	"admin.notification.test": "Když správce pošle zkušební oznámení",
}

// ActionLabel returns the human Czech phrase for an action, falling back to the
// qualified key so a newly-added module action is still pickable (unlabelled)
// rather than invisible.
func ActionLabel(module, key string) string {
	if label, ok := actionLabels[module+"."+key]; ok {
		return label
	}
	return module + "." + key
}
