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
	// v9 — publishing a private item into the shared tree.
	"notes.note.publish":   "Když někdo zveřejní soukromou poznámku pro celou domácnost",
	"notes.folder.publish": "Když někdo zveřejní soukromou složku poznámek pro celou domácnost",

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
	// v9 — publishing a private item into the shared tree.
	"documents.document.publish":        "Když někdo zveřejní soukromý dokument pro celou domácnost",
	"documents.document_folder.publish": "Když někdo zveřejní soukromou složku dokumentů pro celou domácnost",

	// finance (Finance)
	"finance.month.create": "Když někdo zadá měsíc do Financí",
	"finance.month.update": "Když někdo upraví měsíc ve Financích",
	"finance.month.delete": "Když někdo trvale smaže měsíc z Financí",

	// garden (Zahrada)
	"garden.plant.create":    "Když někdo přidá rostlinu",
	"garden.plant.update":    "Když někdo upraví rostlinu",
	"garden.plant.delete":    "Když někdo smaže rostlinu",
	"garden.variety.create":  "Když někdo přidá odrůdu",
	"garden.variety.update":  "Když někdo upraví odrůdu",
	"garden.variety.delete":  "Když někdo smaže odrůdu",
	"garden.bed.create":      "Když někdo přidá záhon",
	"garden.bed.update":      "Když někdo upraví záhon",
	"garden.bed.delete":      "Když někdo smaže záhon",
	"garden.bed.move":        "Když někdo přesune záhon",
	"garden.season.create":   "Když někdo založí sezónu",
	"garden.season.update":   "Když někdo upraví sezónu",
	"garden.season.close":    "Když někdo uzavře sezónu",
	"garden.season.reopen":   "Když někdo znovu otevře sezónu",
	"garden.planting.create": "Když někdo přidá výsadbu",
	"garden.planting.update": "Když někdo upraví výsadbu",
	"garden.planting.delete": "Když někdo smaže výsadbu",
	"garden.task.create":     "Když někdo přidá zahradní úkol",
	"garden.task.update":     "Když někdo upraví zahradní úkol",
	"garden.task.delete":     "Když někdo smaže zahradní úkol",
	"garden.harvest.create":  "Když někdo zapíše sklizeň",
	"garden.harvest.update":  "Když někdo upraví sklizeň",
	"garden.harvest.delete":  "Když někdo smaže sklizeň",
	"garden.storage.create":  "Když někdo přidá uskladnění úrody",
	"garden.storage.update":  "Když někdo upraví uskladnění úrody",
	"garden.storage.delete":  "Když někdo smaže uskladnění úrody",
	"garden.rule.create":     "Když někdo vytvoří zahradní pravidlo",
	"garden.rule.update":     "Když někdo upraví zahradní pravidlo",
	"garden.rule.delete":     "Když někdo smaže zahradní pravidlo",
	"garden.settings.update": "Když někdo změní nastavení zahrady",
	"garden.frost_warning":   "Když se vydá výstraha před mrazem",

	// electricity (Elektřina)
	"electricity.reading.create": "Když někdo přidá odečet elektřiny",
	"electricity.reading.update": "Když někdo upraví odečet elektřiny",
	"electricity.reading.delete": "Když někdo smaže odečet elektřiny",
	"electricity.tariff.create":  "Když někdo vytvoří tarif",
	"electricity.tariff.update":  "Když někdo upraví tarif",
	"electricity.tariff.delete":  "Když někdo smaže tarif",
	"electricity.advance.create": "Když někdo přidá zálohu na elektřinu",
	"electricity.advance.update": "Když někdo upraví zálohu na elektřinu",
	"electricity.advance.delete": "Když někdo smaže zálohu na elektřinu",
	"electricity.payment.create": "Když někdo přidá platbu za elektřinu",
	"electricity.payment.update": "Když někdo upraví platbu za elektřinu",
	"electricity.payment.delete": "Když někdo smaže platbu za elektřinu",
	"electricity.period.create":  "Když někdo vytvoří zúčtovací období",
	"electricity.period.update":  "Když někdo upraví zúčtovací období",
	"electricity.period.delete":  "Když někdo smaže zúčtovací období",

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
	// v9 — the only READ in Home that writes an audit event. Opening the list of
	// other members' private items is recorded, because "who looked" is the answer
	// the household is owed for a screen that exists at all (D198).
	"admin.private_items.view": "Když správce otevře seznam soukromých položek",

	// chat (Chat) — v10. ⚠ ELEVEN STRUCTURAL VERBS AND NO MESSAGE VERB (D231).
	// Sending, editing and deleting a message write nothing to the Log, so there is
	// deliberately no "Když někdo pošle zprávu" here to bind a rule to. The gap is
	// asserted by TestChatMessagesAreNotAudited rather than only described.
	//
	// ⚠ AND ATTACHMENTS ARE HERE ALTHOUGH THE MESSAGES CARRYING THEM ARE NOT. The
	// bytes are what the two thresholds, the clean-up page and the storage register
	// exist for; "who uploaded that 40 MB video, and when" is the question the whole
	// storage half of v10 answers, and it is not answerable from a message log that
	// does not exist.
	"chat.conversation.created":  "Když někdo vytvoří konverzaci",
	"chat.conversation.renamed":  "Když někdo přejmenuje konverzaci",
	"chat.conversation.deleted":  "Když někdo přesune konverzaci do koše",
	"chat.conversation.restored": "Když někdo obnoví konverzaci z koše",
	"chat.conversation.purged":   "Když někdo smaže konverzaci natrvalo",
	"chat.member.added":          "Když někdo přidá člena do konverzace",
	"chat.member.removed":        "Když někdo odebere člena z konverzace",
	"chat.attachment.uploaded":   "Když někdo nahraje soubor do konverzace",
	"chat.attachment.removed":    "Když někdo odstraní soubor při úklidu úložiště",
	"chat.attachment.moved":      "Když někdo přesune soubor z chatu do Dokumentů",
	// ⚠ `threshold.update`, not `settings.updated` (D263 — the design bundle is
	// later than the PRD and wins on this point). Emitted by the admin module and
	// filed under `chat`, because the setting is an admin's to change and the
	// subject is chat's.
	"chat.threshold.update": "Když správce změní limity úložiště chatu",
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
