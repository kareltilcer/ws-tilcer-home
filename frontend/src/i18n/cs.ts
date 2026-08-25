// Centralized Czech UI copy (PRD D20: Czech-only, no i18n framework, strings in
// one place). Add module strings here as screens are built.

export const cs = {
  app: {
    name: 'home',
    redirecting: 'Ověřování přihlášení…',
    signOut: 'Odhlásit se',
  },
  login: {
    heading: 'Přihlášení',
    subtitle: 'Přihlaste se ke svému domácímu účtu.',
    email: 'E-mail',
    password: 'Heslo',
    submit: 'Přihlásit',
    forgot: 'Zapomněli jste heslo?',
    noAccount: 'Nemáte účet? Požádejte správce.',
    errCreds: 'Neplatný e-mail nebo heslo.',
    errDisabled: 'Účet je zablokován nebo nemá přístup.',
    errServer: 'Ověřovací služba je dočasně nedostupná. Zkuste to prosím znovu.',
    mfaTitle: 'Tento účet vyžaduje dvoufázové ověření.',
    mfaLink: 'Pokračovat na auth.tilcer.cz',
  },
  nav: {
    nastenka: 'Nástěnka',
    ukoly: 'Úkoly',
    okno: 'Okno do budoucnosti',
    oknoShort: 'Okno',
    poznamky: 'Poznámky',
    dokumenty: 'Dokumenty',
    log: 'Log',
    more: 'Více',
    moreHeading: 'Další',
    // v4 (D49): with Dokumenty there are five member destinations, so the overflow
    // sheet is no longer admin-only — the four daily modules stay on the bar and
    // everyone finds Dokumenty (and admins the Log) here.
    moreHint: 'Čtyři denní moduly zůstávají v dosahu palce. Zbytek je tady.',
    logDesc: 'Auditní historie — jen pro správce',
    dokumentyDesc: 'Soubory — náhled, stažení, připnutí',
    // v6 (D84): Finance joins the overflow for EVERYONE — no admin gate, unlike
    // Log and Administrace.
    finance: 'Finance',
    financeDesc: 'Rozdělení měsíčních příjmů',
    // v7: Zahrada joins the same overflow for everyone — and is the first module
    // that is a doorway to eight sub-pages rather than to one screen.
    zahrada: 'Zahrada',
    zahradaDesc: 'Plodiny, plán sezóny a práce na zahradě',
    // v8: the description answers the QUESTION the module answers, not what it
    // contains. Nothing anywhere else will ever mention Elektřina — no widget, no
    // notification — so this line has to earn the trip on its own.
    elektrina: 'Elektřina',
    elektrinaDesc: 'Odečty, ceník a zálohy — vyjdou, nebo doplatím?',
  },
  common: {
    loading: 'Načítání…',
    errorTitle: 'Něco se pokazilo',
    retry: 'Zkusit znovu',
    readOnly: 'Jen pro čtení',
    accessDenied: 'Přístup odepřen',
    accessDeniedDetail: 'Tato sekce je jen pro administrátory.',
    empty: 'Nic tu zatím není',
    folderIcon: 'Ikona složky',
    optional: 'nepovinné',
    customIcon: 'Vlastní ikona',
    browseEmoji: 'Procházet všechny emoji',
    emojiSearchPlaceholder: 'Hledat emoji…',
    emojiNoResults: 'Žádné emoji nenalezeno',
  },
  live: {
    // Shown when a websocket push made elsewhere (another device or tab) changes
    // what's on the screen the user is currently looking at — so the live update
    // isn't a silent surprise. "mezitím" (in the meantime) fits both cases.
    tasksUpdated: 'Úkoly byly mezitím upraveny',
    eventsUpdated: 'Události byly mezitím upraveny',
    notesUpdated: 'Poznámky byly mezitím upraveny',
    documentsUpdated: 'Dokumenty byly mezitím upraveny',
    financeUpdated: 'Finance byly mezitím upraveny',
    gardenUpdated: 'Zahrada byla mezitím upravena',
    electricityUpdated: 'Elektřina byla mezitím upravena',
  },
  dashboard: {
    title: 'Nástěnka',
    subtitle: 'Co teď potřebuje tvou pozornost',
    remindersHeading: 'Události',
    tasksHeading: 'Úkoly',
    emptyTitle: 'Nástěnka je prázdná',
    emptyBody: 'Přidejte si widget a mějte přehled o tom, co vás čeká.',
    arrange: 'Uspořádat',
    arrangeDone: 'Hotovo',
    addWidget: 'Přidat widget',
    narrow: 'Úzký',
    wide: 'Široký',
    catalogTitle: 'Widgety',
    shown: 'Zobrazeno',
    add: 'Přidat',
  },
  notes: {
    title: 'Poznámky',
    subtitle: 'Markdown poznámky ve složkách — sdílené v domácnosti',
    search: 'Hledat v poznámkách…',
    newFolder: 'Složka',
    newNote: 'Poznámka',
    createNoteFull: 'Vytvořit poznámku',
    noteHere: 'Poznámka sem',
    root: 'Poznámky',
    tree: 'Strom složek',
    readOnly: 'Jen pro čtení',
    // states
    emptyRootTitle: 'Zatím žádné poznámky',
    emptyRootBody: 'Vytvořte první poznámku nebo si obsah roztřiďte do složek.',
    emptyFolderTitle: 'Prázdná složka',
    emptyFolderBody: 'Zatím tu nic není.',
    noResultsTitle: 'Nic nenalezeno',
    noResultsBody: 'Zkus jiný výraz.',
    errorTitle: 'Poznámku se nepodařilo načíst',
    pickPrompt: 'Vyberte poznámku ze stromu složek.',
    // editor / view
    modeRead: 'Číst',
    modeVisual: 'Vizuální',
    modeMarkdown: 'Markdown',
    changedElsewhere: 'Tuto poznámku mezitím upravil někdo jiný. Tvoje změny se uloží jako poslední (přepíší).',
    reloadTheirs: 'Načíst jejich verzi',
    saving: 'Ukládám…',
    saved: 'Uloženo',
    saveError: 'Nepodařilo se uložit',
    saveRejected: 'Server tuto verzi poznámky odmítl. Koncept zůstal zachovaný — uprav ji a zkus to znovu.',
    draftRecovered: 'Obnovili jsme neuložený koncept této poznámky.',
    imageUploadError: 'Obrázek se nepodařilo nahrát',
    imageMigrated: 'Obrázek vložený přímo v textu jsme nahráli do úložiště.',
    imageUploadBlocked:
      'Obrázek vložený přímo v textu této poznámky se nepodařilo nahrát do úložiště, takže poznámku nejde uložit, dokud v ní zůstane. Otevři režim Markdown a obrázek z textu odeber.',
    imageNotEmbeddable: 'Vložený obrázek nejde nahrát ani uložit přímo v textu. Poznámka půjde uložit až bez něj.',
    goneTitle: 'Poznámka už není dostupná',
    goneBody: 'Někdo ji mezitím smazal. Zavři toto okno.',
    bodyPlaceholder: 'Piš v Markdownu…',
    // organisation
    rename: 'Přejmenovat',
    move: 'Přesunout',
    moveNoteInto: 'Přesunout poznámku do…',
    moveFolderInto: 'Přesunout složku do…',
    delete: 'Smazat',
    cancel: 'Zrušit',
    createFolderHeading: 'Nová složka',
    createNoteHeading: 'Nová poznámka',
    renameFolderHeading: 'Přejmenovat složku',
    folderNamePlaceholder: 'Název složky',
    noteNamePlaceholder: 'Název poznámky',
    locationLabel: 'Ve složce:',
    rootLocation: 'Poznámky (kořen)',
    submitCreate: 'Vytvořit',
    submitRename: 'Uložit',
    // delete confirm
    deleteNoteTitle: (t: string) => `Smazat poznámku „${t}“?`,
    deleteNoteBody: 'Poznámku nelze vrátit zpět. Historie zůstane v auditním logu.',
    deleteFolderTitle: (t: string) => `Smazat složku „${t}“?`,
    deleteFolderEmpty: 'Prázdnou složku lze bezpečně smazat.',
    deleteFolderCascade: 'Smaže se i obsah složky (podsložky a poznámky). Akci nelze vzít zpět.',
    confirmDelete: 'Smazat',
    // pinning
    pin: 'Připnout',
    pinned: 'Připnuto',
    pinHousehold: 'Připnout pro všechny',
    pinHouseholdHint: 'Sdílené — vidí celá domácnost',
    pinPersonal: 'Připnout jen pro mě',
    pinPersonalHint: 'Osobní — jen tvoje zařízení',
    badgeHousehold: 'Připnuto pro všechny',
    badgePersonal: 'Jen pro mě',
    badgeBoth: 'Pro všechny',
    // sharing
    copyLink: 'Odkaz',
    copyLinkTitle: 'Zkopírovat odkaz — otevře se jen přihlášeným členům domácnosti',
    copyLinkDone: 'Odkaz zkopírován — otevře se jen přihlášeným členům domácnosti',
    copyLinkError: 'Odkaz se nepodařilo zkopírovat',
    copyLinkNote: 'Přejmenování nebo přesun poznámky odkaz změní.',
    // widget
    widgetEmpty: 'Žádné připnuté poznámky.',
  },
  documents: {
    title: 'Dokumenty',
    subtitle: 'Soubory ve složkách — trvalý odkaz, náhled i stažení',
    search: 'Hledat (název + popis)…',
    // Search covers metadata only — say so, so nobody expects full-text search.
    searchScope: 'Hledání v názvech a popisech — ne v obsahu souborů',
    root: 'Dokumenty',
    rootLocation: 'Dokumenty (kořen)',
    tree: 'Strom složek',
    newFolder: 'Složka',
    upload: 'Nahrát dokument',
    uploadShort: 'Nahrát',
    viewList: 'Seznam',
    viewGrid: 'Mřížka',
    readOnly: 'Jen pro čtení',
    readOnlyHint: 'náhled a stažení k dispozici',
    // states
    emptyRootTitle: 'Zatím žádné dokumenty',
    emptyRootBody: 'Nahrajte první dokument — PDF, obrázek nebo soubor z Office.',
    emptyFolderTitle: 'Prázdná složka',
    emptyFolderBody: 'Zatím tu nejsou žádné dokumenty.',
    noResultsTitle: 'Nic nenalezeno',
    noResultsBody: 'Zkus jiný výraz. Prohledáváme názvy a popisy, ne obsah.',
    errorTitle: 'Nepodařilo se načíst dokumenty',
    pickPrompt: 'Vyberte dokument ze stromu složek.',
    goneTitle: 'Dokument už není dostupný',
    goneBody: 'Někdo ho mezitím smazal. Zavři toto okno.',
    // viewer
    download: 'Stáhnout',
    downloadOriginal: 'Stáhnout originál',
    immutable: 'neměnný — nová verze = nový dokument',
    previewPendingTitle: 'Náhled se připravuje…',
    previewPendingBody:
      'Dokument z Office převádíme na PDF. Objeví se automaticky, jakmile bude hotový. Mezitím lze soubor stáhnout.',
    previewFailedBody: 'Náhled se nepodařilo vytvořit. Soubor je v pořádku — lze ho stáhnout.',
    previewNoneBody: 'Pro tento typ souboru náhled neumíme. Stáhni si ho a otevři v aplikaci.',
    previewTooLargeBody: (mb: number) =>
      `Textový soubor je větší než ${mb} MB — náhled v prohlížeči by ho zbytečně zdržel. Stáhni si ho.`,
    previewOpenInTab: 'Otevřít v novém okně',
    previewPinchHint: 'Sevřením lze přiblížit',
    previewFrameLabel: 'Náhled dokumentu',
    chipPending: 'Náhled se připravuje…',
    chipFailed: 'Bez náhledu',
    chipDownloadOnly: 'Jen ke stažení',
    // upload queue
    uploadHeading: 'Nahrávání',
    uploadDropHint: 'Přetáhni soubory sem',
    uploadDone: 'Nahráno',
    uploadFailed: 'Nepodařilo se nahrát',
    uploadTooLarge: (mb: number) => `Soubor je větší než ${mb} MB.`,
    uploadRejectedType: 'Tento typ souboru nepřijímáme.',
    uploadPreviewPending: 'Náhled se připravuje…',
    uploadRemove: 'Odebrat',
    uploadCloseWhenDone: 'Zavřít',
    // organisation
    rename: 'Přejmenovat / popis',
    renameHeading: 'Přejmenovat dokument',
    descriptionLabel: 'Popis (nepovinný)',
    descriptionPlaceholder: 'Např. kde v dokumentu najdeš číslo smlouvy',
    move: 'Přesunout',
    moveDocumentInto: 'Přesunout dokument do…',
    moveFolderInto: 'Přesunout složku do…',
    moveDone: 'Dokument přesunut — odkaz zůstává stejný',
    delete: 'Smazat',
    cancel: 'Zrušit',
    createFolderHeading: 'Nová složka',
    renameFolderHeading: 'Přejmenovat složku',
    folderNamePlaceholder: 'Název složky',
    documentNamePlaceholder: 'Název dokumentu',
    locationLabel: 'Ve složce:',
    submitCreate: 'Vytvořit',
    submitRename: 'Uložit',
    saveError: 'Nepodařilo se uložit',
    emptyFolderLabel: 'prázdná',
    // delete confirm — soft vs the admin hard delete that also removes the file
    deleteDocumentTitle: (t: string) => `Smazat dokument „${t}“?`,
    deleteDocumentBody: 'Dokument se archivuje a zmizí ze složek. Historie zůstane v auditním logu.',
    deleteFolderTitle: (t: string) => `Smazat složku „${t}“?`,
    deleteFolderEmpty: 'Prázdnou složku lze bezpečně smazat.',
    deleteFolderCascade: 'Smaže se i obsah složky (podsložky a dokumenty). Akci nelze vzít zpět.',
    // 409 on a trvalé smazání: somewhere in the folder's subtree sit archived rows
    // a hard delete would purge for good. They never show in the tree, so the
    // confirm dialog could not have counted them — hence the refusal.
    deleteFolderArchivedConflict: 'Složka obsahuje dříve smazané dokumenty. Trvalé smazání by je odstranilo i se soubory — smaž je nejdřív jednotlivě.',
    // 409 "conflict": the folder turned out not to be empty, i.e. the tree the dialog
    // counted from was stale. It is refetched before this shows, so confirming again
    // deletes the content the dialog now names.
    deleteFolderStaleConflict: 'Ve složce mezitím něco přibylo. Zkontroluj obsah a potvrď smazání znovu.',
    confirmDelete: 'Smazat',
    hardDeleteLabel: 'Smazat trvale i soubor',
    hardDeleteHint: 'Nevratné — odstraní i uložený soubor. Jen pro správce.',
    hardDeleteDone: 'Dokument trvale smazán (i soubor)',
    softDeleteDone: 'Dokument archivován',
    hardDeleteFolderDone: 'Složka trvale smazána (i soubory)',
    softDeleteFolderDone: 'Složka archivována',
    // pinning — `pin` is the button (imperative), `pinned`/`unpinned` are the
    // confirmations (past tense); a toast must never use the imperative form.
    pin: 'Připnout',
    pinned: 'Připnuto',
    unpinned: 'Odepnuto',
    pinHousehold: 'Připnout pro všechny',
    pinHouseholdHint: 'Sdílené — vidí celá domácnost',
    pinPersonal: 'Připnout jen pro mě',
    pinPersonalHint: 'Osobní — jen tvoje zařízení',
    badgeHousehold: 'Připnuto pro všechny',
    badgePersonal: 'Jen pro mě',
    badgeBoth: 'Pro všechny',
    // sharing — the documents link is PERMANENT, unlike the notes one
    copyLink: 'Odkaz',
    copyLinkTitle: 'Zkopírovat trvalý odkaz — otevře se jen přihlášeným členům domácnosti',
    copyLinkDone: 'Trvalý odkaz zkopírován — otevře se jen členům domácnosti',
    copyLinkError: 'Odkaz se nepodařilo zkopírovat',
    // Deliberately quotes no path: the permalink is /d/{full id}, and printing a
    // shortened one in a 236px popover would just be a link that 404s if typed out.
    copyLinkNote: 'Zkopírovaný odkaz je trvalý — nemění se při přejmenování ani přesunu.',
    // widget
    widgetEmpty: 'Žádné připnuté dokumenty.',
    overlayTitle: 'Dokument · z Nástěnky',
  },

  // ---- v5: Nastavení → Oznámení (every role, incl. reader) ----
  settings: {
    title: 'Nastavení',
    subtitle: 'Oznámení na tomto zařízení · vzhled · aplikace',
    notifications: 'Oznámení',
    thisDevice: 'toto zařízení',
    thisDeviceHint: 'Telefon i notebook si nastavuješ zvlášť.',
    // Priming: the browser prompt is one-shot, so it fires only on intent.
    primingBody:
      'Oznámení chodí na to zařízení, kde si je zapneš — třeba když se blíží připomínka nebo přijde ranní přehled.',
    enable: 'Zapnout oznámení na tomto zařízení',
    enabled: 'Oznámení jsou na tomto zařízení zapnutá',
    disable: 'Vypnout na tomto zařízení',
    dismissed: 'Dialog jsi zavřel(a) bez rozhodnutí — zkusit můžeš znovu.',
    blockedTitle: 'Oznámení jsou blokovaná v prohlížeči',
    blockedBody:
      'Povolení jsme se už jednou zeptali a bylo zamítnuté — z aplikace se znovu zeptat nemůžeme. Otevři v prohlížeči nastavení pro home.tilcer.cz a povol Oznámení; pak se sem vrať.',
    unsupportedTitle: 'Tento prohlížeč oznámení nepodporuje',
    unsupportedBody:
      'Web Push tady není k dispozici. Na iPhonu a iPadu funguje až po přidání aplikace na plochu.',
    // The browser has the Push API but never gave us a service worker — typically
    // an anonymous window. Said BEFORE the permission prompt is spent, so the
    // member still has their one chance left in a normal window.
    swUnavailable:
      'Prohlížeč nespustil aplikaci na pozadí, bez které oznámení chodit nemohou. V anonymním okně to nefunguje — zkus to v běžném okně.',
    serverDisabledTitle: 'Oznámení nejsou na serveru nastavená',
    serverDisabledBody: 'Správce zatím nenastavil klíče pro odesílání oznámení.',
    offlineHint: 'Zapnutí oznámení vyžaduje připojení.',
    masterLabel: 'Oznámení',
    masterHint: 'Hlavní vypínač pro toto zařízení',
    catBroadcast: 'Rozeslaná oznámení',
    catBroadcastHint: 'Zprávy, které rozešle správce.',
    catTriggers: 'Upozornění na akce',
    catTriggersHint: 'Když se v domácnosti něco změní.',
    catSummaries: 'Souhrny',
    catSummariesHint: 'Pravidelné přehledy, třeba ranní.',
    selfTest: 'Poslat zkušební oznámení',
    selfTestSent: 'Zkušební oznámení odesláno na toto zařízení.',
    selfTestFailed: 'Zkušební oznámení se nepodařilo doručit. Zkuste oznámení vypnout a znovu zapnout.',
    appearance: 'Vzhled',
    theme: 'Motiv',
    app: 'Aplikace',
    install: 'Nainstalovat aplikaci',
    installHint:
      'Na telefonu „Přidat na plochu“. Aplikace se otevírá v tmavém provedení bez adresního řádku.',
    offlineNote:
      'Bez připojení si Home otevřeš a přečteš poslední načtená data. Ukládat změny, otevírat náhledy dokumentů ani se přihlašovat offline nelze.',
  },

  // ---- v5: Administrace (admin only) ----
  admin: {
    title: 'Administrace',
    subtitle: 'Oznámení pro domácnost — co se posílá a komu',
    navDesc: 'Oznámení — rozeslat, pravidla, souhrny',
    tabSend: 'Rozeslat',
    tabRules: 'Pravidla',
    tabSummaries: 'Souhrny',
    tabDeliveries: 'Doručení',

    // composer
    composerTitle: 'Nadpis',
    composerBody: 'Text',
    insertToken: 'Vložit údaj',
    preview: 'Živý náhled',
    previewHint: 'Takhle oznámení uvidí příjemci — údaje jsou vyplněné ukázkovými hodnotami.',
    send: 'Odeslat',
    sendTest: 'Poslat test',
    sending: 'Odesílám…',
    testSent: 'Zkušební oznámení odesláno na tvoje zařízení.',
    tokensTime: 'Čas a datum',
    tokensEvent: 'Údaje o akci',
    tokensChange: 'Změněná hodnota',
    tokensMetric: 'Čísla z modulů',
    tokensList: 'Seznamy z modulů',
    // A list and the metric it names share a label, so the chip says which it is
    // — the group heading is not announced with the button.
    tokensListSuffix: '(seznam)',

    // audience
    audience: 'Komu',
    audienceAll: 'Všem',
    audienceRoles: 'Podle role',
    audienceUsers: 'Vybraným lidem',
    audienceEchoAll: 'Odejde všem',
    audienceEmpty: 'Vyber aspoň jednoho příjemce.',
    nobodySubscribed: 'Zatím nemá oznámení zapnuté nikdo — zprávu můžeš napsat, ale nikam nedorazí.',

    // broadcast
    sendHeading: 'Rozeslat oznámení',
    sendHint: 'Jednorázová zpráva pro domácnost. Neukládá se jako pravidlo.',
    sentTitle: 'Odesláno',

    // rules
    rulesHeading: 'Pravidla',
    rulesHint: 'Když se v domácnosti něco stane, přijde oznámení.',
    rulesEmpty: 'Zatím žádná pravidla',
    rulesEmptyHint: 'Vytvoř první pravidlo — třeba upozornění, když někdo dokončí připomínku.',
    newRule: 'Nové pravidlo',
    ruleName: 'Název pravidla',
    ruleTrigger: 'Kdy se má poslat',
    ruleTriggerPlaceholder: 'Vyber akci…',
    ruleTriggerRequired: 'Vyber akci, na kterou má pravidlo reagovat.',
    ruleFilters: 'Zúžit (nepovinné)',
    ruleBodyPlaceholder: 'Necháš-li prázdné, pošle se popis akce z logu.',
    coalesce: 'Sloučit opakování',
    coalesceHint: 'Stejné akce v krátkém sledu spojí do jednoho oznámení.',
    coalesceOff: 'Neslučovat — poslat každou akci zvlášť',
    notifyActor: 'Upozornit i původce akce',
    notifyActorHint: 'Vypni, když nechceš, aby oznámení chodilo tomu, kdo změnu udělal.',
    enabledLabel: 'Zapnuto',

    // conditions ("poslat jen když")
    conditions: 'Poslat jen když (nepovinné)',
    conditionsHintRule: 'Oznámení odejde, jen když podmínky platí ve chvíli odeslání.',
    conditionsHintSummary:
      'Souhrn se pošle, jen když podmínky platí v naplánovaný čas. Osobní údaje se vyhodnocují pro každého příjemce zvlášť.',
    conditionAdd: 'Přidat podmínku',
    conditionKeyPlaceholder: 'Vyber údaj…',
    conditionModeAll: 'musí platit všechny podmínky',
    conditionModeAny: 'stačí jedna podmínka',
    conditionRemove: 'Odebrat podmínku',
    conditionIncomplete: 'Vyber údaj v každé podmínce.',
    opGt: 'je víc než',
    opGte: 'je aspoň',
    opLt: 'je míň než',
    opLte: 'je nejvýš',
    opEq: 'je přesně',
    opNeq: 'není',

    // active window (rules)
    activeWindow: 'Posílat jen v určitou dobu (nepovinné)',
    activeWindowHint:
      'Mimo tuto dobu se oznámení nepošle. Konec před začátkem znamená okno přes půlnoc (např. 20:00–06:00).',
    activeFrom: 'Od',
    activeTo: 'Do',
    activeWindowIncomplete: 'Vyplň oba časy, nebo žádný.',
    ruleConditionsBadge: 'jen za podmínek',
    ruleWindowBadge: 'jen',

    // schedules
    summariesHeading: 'Souhrny',
    summariesHint: 'Pravidelný přehled v určený čas.',
    summariesEmpty: 'Zatím žádné souhrny',
    summariesEmptyHint: 'Vytvoř první souhrn — třeba ranní přehled v 8:00.',
    newSummary: 'Nový souhrn',
    summaryName: 'Název souhrnu',
    scheduleTime: 'Čas',
    scheduleDays: 'Kdy',
    dayDaily: 'Každý den',
    dayWeekdays: 'Všední dny',
    dayWeekends: 'Víkend',
    dayPicked: 'Vybrané dny',
    dayOfMonth: 'N-tého v měsíci',
    dayOfMonthLabel: 'Den v měsíci',
    // D74: 29–31 clamp to the month's last day rather than skipping short months.
    dayOfMonthClampHint: 'V kratších měsících se posune na poslední den měsíce.',
    perRecipientNote:
      'Údaje označené „osobní“ se počítají pro každého příjemce zvlášť. Seznam se do oznámení vypíše po řádcích a delší se zkrátí.',

    // deliveries
    deliveriesHeading: 'Doručení',
    // Deliberately explicit: this LOOKS like the Log but means something else.
    deliveriesHint:
      'Provozní záznam o odeslání — best effort, ne auditní log. Starší záznamy se automaticky mažou.',
    deliveriesEmpty: 'Zatím nic neodešlo',
    kindBroadcast: 'Rozeslané',
    kindTrigger: 'Pravidlo',
    kindSchedule: 'Souhrn',
    kindTest: 'Test',
    statusSent: 'Odesláno',
    statusFailed: 'Nedoručeno',
    statusExpired: 'Vypršelo',
    colTime: 'Čas',
    colKind: 'Typ',
    colRecipient: 'Příjemce',
    colStatus: 'Stav',
    filterAll: 'Vše',
    loadMore: 'Načíst další',

    // shared
    save: 'Uložit',
    saving: 'Ukládám…',
    cancel: 'Zrušit',
    delete: 'Smazat',
    deleteConfirm: 'Opravdu smazat?',
    saveError: 'Nepodařilo se uložit',
  },

  // ---- v6: Finance ----
  //
  // The vocabulary below is FIXED IN THE PRD (§V6-7) and used verbatim, so the
  // page, the widget, the metric labels and the notification tokens all say the
  // same words. "Kandy" stays: it is the household's own name for the joint
  // account, and translating it away would make the app read as somebody else's.
  finance: {
    title: 'Finance',
    lede: 'Měsíční příjmy rozdělené na osobní účty, provozní účet a spoření.',
    months: 'Měsíce',
    add: 'Přidat měsíc',
    edit: 'Upravit měsíc',
    save: 'Uložit změny',
    cancel: 'Zrušit',
    remove: 'Smazat',
    formLede: 'Dva příjmy a čtyři sazby. Zbytek se dopočítá.',

    // inputs
    month: 'Měsíc',
    monthTaken: '— už zadaný',
    monthTakenHint: 'Už zadané měsíce nejdou vybrat — upravte je v seznamu.',
    income: 'Příjem',
    incomeKaja: 'Příjem Kája',
    incomeAndy: 'Příjem Andy',
    // Not "neplatná hodnota": it says what a valid one looks like, because the
    // usual mistake is a tečka ("60.000") that would otherwise save as 60 Kč.
    incomeInvalid: 'Příjem zadejte číslicemi — tisíce oddělte mezerou, desetiny čárkou (např. 60 000).',
    rates: 'Sazby',
    ratePersonal: 'Osobní',
    rateOperational: 'Provozní',
    rateFun: 'Zábavné spoření',
    rateNoFun: 'Nezábavné spoření',
    ratesMustSum: 'Sazby musí dát dohromady 100 %.',
    ratesOk: 'sedí — 100 %',
    // A running remainder rather than a validation slap: the form tells you where
    // you are the whole time, instead of refusing at the end.
    ratesRemaining: (n: number) => `zbývá ${n} %`,
    ratesOver: (n: number) => `přebývá ${n} %`,
    ratesBalance: 'Doplnit do 100 % (Nezábavné)',

    // the split, as the flow viz and the bar name it
    totalIncome: 'Celkový příjem',
    allocation: 'Rozdělení',
    personal: 'Osobní',
    needs: 'Potřeby',
    operational: 'Provozní účet (Kandy)',
    operationalNeeds: 'Provozní účet (Kandy) · potřeby',
    funSavings: 'Zábavné spoření',
    noFunSavings: 'Nezábavné spoření',
    toSavings: 'Do spoření',
    restToKandy: 'Zbytek → Kandy',
    stageIncome: 'Příjem',
    stagePersonal: (p: number) => `Každý si nechá ${p} % osobně`,
    stageJoint: 'Společné účty',
    ofIncome: (n: number) => `${n} % z příjmu`,
    ofTotal: (n: number) => `${n} % z celku`,
    operationalNote: (received: string) => `přijato ${received}, spoření odchází`,
    noIncomeThisMonth: 'V tomto měsíci bez příjmu — do společných účtů nepřispívá.',
    reconciliation: (total: string) =>
      `Potřeby pohlcují zaokrouhlení, takže součty za osobu i za účet sedí přesně na ${total}.`,
    // A negative `needs` is correct arithmetic, not an error: the UI shows 0 Kč
    // and states the rounding underneath. The value is never clamped in the data.
    roundingFootnote: (v: string) => `zaokrouhlení: ${v}`,
    previewTitle: 'Náhled rozdělení',
    previewNegative: (v: string) =>
      `Potřeby vyjdou na ${v} — ukazujeme 0 Kč a zaokrouhlení uvádíme pod hodnotou.`,

    // stat tiles — a big value, a quiet label, one line of context; no sparklines
    tileLatest: 'Poslední měsíc',
    tileLatestMeta: (month: string) => `${month} · celkový příjem`,
    tileSaved: 'Naspořeno celkem',
    tileSavedMeta: (fun: string, nofun: string) => `zábavné ${fun} / nezábavné ${nofun}`,
    tileAverage: 'Průměrný měsíční příjem',
    tileAverageMeta: (months: string) => `Kája + Andy · ${months}`,

    // the missing-month prompt — the module's most valuable pixel
    missingTitle: (month: string) => `${month} ještě nikdo nezadal`,
    missingBody: 'Dokud měsíc chybí, nezobrazí se na Nástěnce ani ve spoření.',
    missingAction: (month: string) => `Zadat ${month}`,
    widgetKajaKeeps: 'Kája si nechá',
    widgetAndyKeeps: 'Andy si nechá',

    // states
    emptyTitle: 'Zatím žádné měsíce',
    emptyBody: 'Přidejte první měsíc a uvidíte rozdělení příjmů.',
    errorTitle: 'Měsíce se nepodařilo načíst',
    errorBody: 'Zkuste to znovu — data zůstala na serveru, nic se neztratilo.',
    monthExists: 'Tento měsíc už je zadaný.',
    saveError: 'Měsíc se nepodařilo uložit',
    created: (month: string) => `Měsíc ${month} přidán`,
    updated: (month: string) => `Měsíc ${month} upraven`,
    deleted: (month: string) => `Měsíc ${month} trvale smazán`,

    // Delete is PERMANENT (D87) — the one exception to a convention the rest of
    // the app has taught, so the copy carries the whole weight.
    deleteTitle: (month: string) => `Smazat měsíc ${month}?`,
    deleteBody:
      'Smazání je trvalé — měsíc se nedá vrátit zpět. Na rozdíl od poznámek a dokumentů se nic nearchivuje.',
    deleteConfirm: 'Smazat trvale',
    deleteError: 'Smazání se nezdařilo',
  },

  // ---- v7: Zahrada ----
  //
  // The vocabulary below is fixed in PRD §V7-7 and used VERBATIM, so the pages,
  // the widget, the metric labels and the notification tokens all say the same
  // words. Everything else is this module's own — and note that its most-read
  // strings are its warnings.
  garden: {
    title: 'Zahrada',
    lede: 'Plodiny, plán sezóny, kontrola plánu a práce — od výsevu po sklad.',

    // The eight sub-pages, in tab-strip order.
    tabs: {
      prehled: 'Přehled',
      plodiny: 'Plodiny',
      zahony: 'Záhony',
      plan: 'Plán',
      kalendar: 'Kalendář',
      sklizen: 'Sklizeň',
      sklad: 'Sklad',
      trvalky: 'Trvalky',
    },

    // §V7-7 vocabulary, verbatim.
    plodina: 'Plodina',
    odruda: 'Odrůda',
    celed: 'Čeleď',
    zahon: 'Záhon',
    castZahrady: 'Část zahrady',
    sezona: 'Sezóna',
    vysadba: 'Výsadba',
    trvalka: 'Trvalka / dřevina',
    kontrolaPlanu: 'Kontrola plánu',
    varovani: 'Varování',
    ignorovat: 'Ignorovat',
    prace: 'Práce',
    terminVysevu: 'Termín výsevu',
    posledniMraz: 'Poslední jarní mráz',
    prvniMraz: 'První podzimní mráz',
    narokNaZiviny: 'Nárok na živiny',
    sklizen: 'Sklizeň',
    sklad: 'Sklad',
    uzavritSezonu: 'Uzavřít sezónu',
    neovereno: 'Neověřeno',

    // First run. The plan is made of beds, so that is where it starts — and the
    // ORDER of those beds is data, not tidiness, which is worth saying up front.
    emptyBedsTitle: 'Zahrada zatím nemá záhony',
    emptyBedsBody:
      'Plán se skládá ze záhonů — začněte tím, že je založíte a srovnáte do pořadí, v jakém opravdu stojí. Sousedství se čte z pořadí.',
    emptyBedsCta: 'Založit první záhon',
    emptySeasonTitle: 'Pro tenhle rok ještě není sezóna',
    emptySeasonBody: 'Sezóna drží očekávané datum mrazu, od kterého se počítají všechny termíny.',
    emptySeasonCta: 'Založit sezónu',
    emptyPlantsTitle: 'Zatím tu nejsou žádné plodiny',
    emptyPlantsBody:
      'Plodina drží, co o ní víte — čeleď, termíny, rozestupy. Můžete ji vyplnit ručně, nebo si nechat pomoct od jazykového modelu.',

    // Kontrola plánu. Severity reads without colour: an icon AND a word.
    severity: {
      error: 'Chyba',
      warn: 'Varování',
      info: 'Poznámka',
      tip: 'Dobře',
    },
    checkClean: 'Plán je bez varování',
    checkCleanBody: 'Nic, co by se teď dalo zlepšit. Kontrola se přepočítá po každé změně plánu.',
    checkGardenWide: 'celá zahrada',
    // The check that CANNOT RUN. Matter-of-fact, not apologetic, and never a
    // green tick (D120).
    noHistoryTitle: 'Rotaci zatím nelze zkontrolovat, chybí historie',
    noHistoryBody:
      'Střídání plodin a sled náročnosti se počítají jen z uzavřených sezón. Po prvním „Uzavřít sezónu“ začnou fungovat — do té doby se nehlásí jako splněné, protože nejsou.',
    dismissTitle: 'Ignorovat varování',
    dismissBody: 'Varování zmizí z panelu pro tuhle sezónu. Zůstane dohledatelné a jde vrátit zpět.',
    dismissNote: 'Poznámka (nepovinná)',
    dismissNotePlaceholder: 'Např. „vím, letos to risknu“',
    dismissConfirm: 'Ignorovat',
    dismissed: 'Ignorováno',
    restore: 'Vrátit zpět',
    showDismissed: 'Zobrazit ignorovaná',

    // Plán.
    planTitle: 'Plán sezóny',
    addPlanting: 'Přidat výsadbu',
    pickCrop: 'Vyberte plodinu',
    pickCropSearch: 'Hledat plodinu…',
    recentlyUsed: 'Naposledy použité',
    bedFree: 'Volný záhon',
    bedUsage: (used: number, total: number) => `${used} z ${total} m²`,
    // Shown instead of bedUsage when a planting is sized by počet rostlin: the
    // card cannot convert that to m² without the crop's density, and "0 z 5 m²"
    // under a list of three crops is worse than saying only what is known.
    bedUsageUnknown: (total: number) => `${total} m²`,
    occupancyAxis: 'Obsazenost v roce',
    copySeason: 'Zkopírovat z předchozí sezóny',
    copyPreview: 'Náhled',
    copyShift: 'Posunout čeledi o záhonů',
    copyBefore: 'Před posunem',
    copyAfter: 'Po posunu',
    seasonClosed: 'Sezóna je uzavřená — jen ke čtení',

    // Výsadba detail.
    planned: 'V plánu',
    actual: 'Ve skutečnosti',
    shiftTasks: 'Posunout navazující práce',
    shiftTasksDone: (n: number) => `Posunuto ${n}`,
    yieldExpected: 'Očekávaný výnos',
    yieldActual: 'Skutečná sklizeň',
    failedTitle: 'Nepovedlo se',
    failReason: 'Co se stalo',

    // Kalendář a práce. A window is written as a RANGE, never a single date.
    calendarTitle: 'Práce na zahradě',
    overdue: 'Zmeškané',
    thisWeek: 'Tento týden',
    week: (label: string) => `Týden ${label}`,
    markDone: 'Hotovo',
    markSkipped: 'Přeskočit',
    reopen: 'Znovu otevřít',
    addTask: 'Přidat práci',
    taskEdited: 'Upraveno ručně',
    taskEditedHint: 'Tuhle práci jste si upravili, plán s ní už nehýbe.',
    filterBed: 'Záhon',
    filterCrop: 'Plodina',
    filterKind: 'Druh práce',
    print: 'Vytisknout',
    printMonth: 'Práce na měsíc',
    printPlan: 'Plán sezóny',

    // Plodiny a odrůdy.
    plantsTitle: 'Plodiny',
    addPlant: 'Přidat plodinu',
    searchPlants: 'Hledat plodinu…',
    onlyUnverified: 'Jen neověřené',
    verify: 'Označit jako ověřené',
    // Bookkeeping, not an accusation — most of it will be fine.
    unverifiedHint: 'Vyplnil jazykový model a nikdo to zatím nezkontroloval.',
    varieties: 'Odrůdy',
    addVariety: 'Přidat odrůdu',
    inherits: 'dědí z plodiny',
    sectionIdentity: 'Základní údaje',
    sectionAgronomy: 'Nároky a půda',
    sectionPropagation: 'Množení a rozestupy',
    sectionTiming: 'Termíny',
    sectionHarvest: 'Sklizeň a skladování',
    sectionProblems: 'Škůdci a choroby',
    sectionNotes: 'Poznámky',

    // The timing-window control — the weirdest input in home.
    anchorWeek: 'Týden v roce',
    anchorLastFrost: 'Vůči poslednímu jarnímu mrazu',
    anchorFirstFrost: 'Vůči prvnímu podzimnímu mrazu',
    windowFrom: 'Od',
    windowTo: 'Do',
    windowWeekHint: 'Týden 1–53, jak se termíny uvádějí v české literatuře.',
    windowFrostHint: 'Počet dní vůči mrazu. Záporné číslo znamená před ním.',
    // The live echo is what makes the abstraction land.
    windowResolved: (range: string) => `Letos vychází na ${range}`,
    windowNoAnchor: 'Sezóna nemá zadané datum mrazu, takže termín zatím nejde dopočítat.',
    windowInverted: 'Konec okna nemůže být dřív než začátek.',

    // The LLM escape hatch.
    promptTitle: 'Nechat vyplnit jazykovým modelem',
    promptBody:
      'Zkopírujte připravený text, vložte ho do libovolného jazykového modelu a jeho odpověď vraťte sem. Nic se neuloží, dokud si náhled neprojdete.',
    promptCopy: 'Zkopírovat text',
    promptCopied: 'Zkopírováno',
    promptPaste: 'Vložte odpověď modelu',
    promptModel: 'Který model odpověděl (nepovinné)',
    previewTitle: 'Náhled importu',
    previewCreate: 'Vytvoří se',
    previewUpdate: 'Změní se',
    previewReject: 'Nepovedlo se přečíst',
    previewUnmapped: 'Pole, kterým nerozumíme',
    previewUnmappedHint: 'Necháváme je stranou a neukládáme je — nic se neztratilo, jen se nepoužije.',
    previewApply: 'Uložit',
    previewSummary: (created: number, updated: number, rejected: number) =>
      `Vytvořeno ${created}, změněno ${updated}, nepovedlo se ${rejected}`,
    exportKb: 'Exportovat plodiny',

    // Záhony — where the drag order MEANS something.
    bedsTitle: 'Záhony',
    addBed: 'Přidat záhon',
    bedCode: 'Značka',
    bedName: 'Název',
    bedType: 'Typ',
    bedArea: 'Plocha',
    bedZone: 'Část zahrady',
    bedNeighbours: 'Sousedí s',
    adjacencyHint:
      'Zahrada nemá mapu. Sousedství se určuje z pořadí záhonů v jedné části: dva po sobě jdoucí záhony jsou sousedi. Když je přesunete, přepočítají se varování o sousedních záhonech — proto je srovnejte v pořadí, v jakém opravdu stojí, ne podle abecedy.',
    moveUp: 'Posunout nahoru',
    moveDown: 'Posunout dolů',
    bedHistory: 'Co tu rostlo',
    bedHistoryEmpty: 'Zatím žádná uzavřená sezóna.',

    // Sklizeň a sklad.
    harvestTitle: 'Sklizeň',
    addHarvest: 'Zapsat sklizeň',
    quickHarvest: 'Rychlý zápis',
    quantity: 'Množství',
    unit: 'Jednotka',
    harvestedOn: 'Datum',
    destination: 'Kam to šlo',
    storageTitle: 'Sklad',
    addStorage: 'Uskladnit',
    productName: 'Co to je',
    method: 'Způsob',
    location: 'Kde',
    remaining: 'Zbývá',
    bestBefore: 'Spotřebovat do',
    // The whole point of the storage log: the number goes down as you eat it.
    consumeHint: 'Zbývající množství upravte, jak ubývá. Když dojde na nulu, položka se sama označí za spotřebovanou.',
    markSpoiled: 'Označit za zkažené',

    // Uzavřít sezónu — a review, not a form.
    closeTitle: 'Uzavřít sezónu',
    closeLede:
      'Projděte, co se letos povedlo a co ne. Po uzavření se sezóna zamkne a začne se z ní počítat střídání plodin — je to jediné, co historii vytváří.',
    closeFrostActual: 'Kdy mráz opravdu přišel',
    closeOutcome: 'Jak to dopadlo',
    closeDone: 'Sklidili jsme',
    closeFailed: 'Nepovedlo se',
    closeFinalYield: 'Celkem sklizeno',
    closeConfirm: 'Uzavřít sezónu',
    closeWarning: 'Po uzavření už do sezóny nepůjde zapisovat. Znovu ji může otevřít jen správce.',
    reopenSeason: 'Znovu otevřít sezónu',
    reopenWarning: 'Otevřením se přepíše historie, ze které se počítá střídání plodin.',

    // Nastavení zahrady. Deliberately no audience or channel settings — that is
    // Administrace's (D113).
    settingsTitle: 'Nastavení zahrady',
    settingsLede: 'Poloha a mrazy, od kterých se odvíjejí všechny termíny.',
    latitude: 'Zeměpisná šířka',
    longitude: 'Zeměpisná délka',
    altitude: 'Nadmořská výška',
    frostThreshold: 'Za mráz považovat teplotu pod',
    rotationDefault: 'Výchozí pauza pro střídání',
    workloadThreshold: 'Nabitý týden začíná od',
    notificationsElsewhere:
      'Kdo a kdy dostane upozornění na mráz se nastavuje v Administraci, ne tady.',
    weatherOff: 'Předpověď je vypnutá — mrazy se berou jen z toho, co zadáte ručně.',
    weatherNoCoords: 'Zadejte polohu a začne se stahovat předpověď.',

    // Widget.
    widgetTitle: 'Práce na zahradě',
    widgetQuiet: 'Na zahradě je teď klid',
    widgetQuietBody: 'Nic nečeká. Od listopadu do února je to tak správně.',

    // Errors.
    loadFailed: 'Zahradu se nepodařilo načíst',
    loadFailedBody:
      'Plán ani zápisy se neztratily. Zkuste to znovu — a pokud jste na zahradě bez signálu, vytiskněte si práci na měsíc.',
  },

  // ---- v5: app-wide offline ----
  offline: {
    banner: 'Jste offline — zobrazená data mohou být starší',
    bannerSub: 'Změny nelze uložit offline. Náhledy dokumentů a přihlášení vyžadují připojení.',
    writeBlocked: 'Změny nelze uložit offline',
    needsConnection: 'Vyžaduje připojení',
  },

  // v8 — Elektřina (PRD §V8-7). The vocabulary in `word` is FIXED by the spec and
  // is used verbatim on the pages, in the forms and in the Log, so a person reads
  // the same noun everywhere.
  //
  // Everything else is owned here, and in this module the COPY DOES WORK THE
  // LAYOUT CANNOT. Four sentences carry most of it: why a prediction is only a
  // prediction, what is missing when one is impossible, what the záloha buys
  // before any consumption is known, and why a stale odečet is an explanation
  // rather than a scolding.
  electricity: {
    title: 'Elektřina',
    lede: 'Odečty, ceník, zálohy a jeden odhad: vyjdou zálohy, nebo doplatím?',

    word: {
      reading: 'Odečet',
      readings: 'Odečty',
      vt: 'VT',
      nt: 'NT',
      vtLong: 'Vysoký tarif',
      ntLong: 'Nízký tarif',
      tariff: 'Ceník',
      priceVT: 'Cena VT',
      priceNT: 'Cena NT',
      monthlyFee: 'Měsíční poplatky',
      advance: 'Záloha',
      advanceSchedule: 'Předpis záloh',
      dueDay: 'Den splatnosti',
      period: 'Zúčtovací období',
      expectedEnd: 'předpokládaný konec',
      invoice: 'Vyúčtování',
      consumption: 'Spotřeba',
      costs: 'Náklady',
      prediction: 'Predikce',
      actualFact: 'Skutečnost',
      underpay: 'Nedoplatek',
      overpay: 'Přeplatek',
      recommended: 'Doporučená záloha',
      addReading: 'Zadat odečet',
      energy: 'Energie',
    },

    tabs: {
      prehled: 'Přehled',
      odecty: 'Odečty',
      cenik: 'Ceníky a poplatky',
      historie: 'Historie a grafy',
    },

    // The headline. The number is confident; the hedge sits above and below it,
    // never inside it — shrinking or greying the figure would trade away the one
    // payoff the module has.
    head: {
      kickerEstimate: 'Odhad k',
      kickerActual: 'Skutečnost k',
      basisPrediction: (days: string) => `predikce z průměru za posledních ${days}`,
      basisActual: (days: string, on: string) =>
        `skutečnost za ${days} — uzavírací odečet k ${on} je zapsaný`,
      costLine: (cost: string, advances: string) =>
        `spočtené náklady ${cost} · zálohy ${advances}`,
      progress: (elapsed: string, remaining: string) => `${elapsed} uplynulo · ${remaining} zbývá`,
    },

    // "Zatím nelze předpovědět" always NAMES what is missing. An unexplained
    // refusal is indistinguishable from a bug.
    cannotPredict: 'Zatím nelze předpovědět',
    reason: {
      need_second_reading: 'potřebuji druhý odečet',
      second_reading_same_day: 'druhý odečet je ze stejného dne jako počáteční',
      no_tariff: 'k začátku období neplatí žádný ceník',
      missing_opening_reading: 'potřebuji odečet k prvnímu dni období',
      tariff_change_inside_interval: 'chybí odečet ke změně ceny',
    },

    // The headroom line — the answer available with ZERO consumption data, and
    // for the first weeks the only real number on the screen.
    headroom: {
      title: 'Co záloha zaplatí',
      line: (forEnergy: string, advance: string, fee: string) =>
        `Ze zálohy ${advance} jdou ${fee} na poplatky, takže na elektřinu zbývá ${forEnergy} měsíčně.`,
      allVT: 'vše ve VT',
      allNT: 'vše v NT',
      mix: 'při 30 % VT a 70 % NT',
      // Named as a guess, because it is one. It is never presented as measured.
      mixNote: 'Poměr 30/70 je odhad, ne měření — skutečný poměr uvidíte po druhém odečtu.',
      missing: 'K začátku období chybí ceník nebo předpis záloh — doplňte je na kartě Ceník.',
    },

    // Blocked is NOT an error: the numbers above the gap are still correct.
    blocked: {
      badge: 'blokováno',
      heading: 'Chybí odečet',
      belowNote: 'Níže uvedené hodnoty nelze spočítat, dokud odečet nedoplníte. Čísla nad tímto místem platí dál.',
      prefill: 'Formulář se předvyplní tímto datem. Hodnoty doplňte podle elektroměru — nic se neodhaduje.',
    },

    // The nudge (D156). Escalation is IN WORDS ONLY: the colour and the size never
    // change, because a line that turns red has started chasing, and chasing is
    // exactly what was refused.
    nudge: {
      line: (days: string) => `Poslední odečet před ${days}`,
      today: 'Poslední odečet je z dneška',
      // A reading DATED IN THE FUTURE is unusual but legal — a mistyped date, or
      // a closing odečet entered ahead of time. "před −42 dny" is nonsense, so
      // this case names the date instead of counting backwards to it.
      future: (on: string) => `Poslední odečet je datovaný k ${on}`,
      futureHint: 'Datum je v budoucnosti — pokud jde o překlep, opravte ho v Odečtech.',
      fresh: 'Čerstvý odečet — predikce stojí na aktuálním průměru.',
      ageing: 'Čím starší odečet, tím starší průměr, ze kterého predikce vychází.',
      stale: 'Predikce zestárla — čísla níž berte jako orientační, dokud nezadáte nový odečet.',
      never: 'Zatím není zapsaný žádný odečet.',
    },

    reading: {
      formTitle: 'Zadat odečet',
      editTitle: 'Upravit odečet',
      date: 'Datum odečtu',
      vt: 'Stav VT (kWh)',
      nt: 'Stav NT (kWh)',
      note: 'Poznámka',
      notePlaceholder: 'např. odečet kvůli změně ceny',
      hint: 'Opište celé kWh z displeje elektroměru — bez desetinné čárky.',
      save: 'Uložit odečet',
      empty: 'Zatím žádné odečty',
      emptyHint: 'První odečet patří k prvnímu dni zúčtovacího období.',
      onlyOne: 'Zatím je zapsaný jediný odečet, takže není co porovnávat. Interval se objeví u druhého.',
      deleteConfirm: 'Smazat odečet?',
      // The Kč on a row is ENERGY ONLY and says so. Poplatky are chunked by
      // (měsíc × ceník) and belong to no single interval.
      intervalEnergy: 'energie',
      intervalDays: 'interval',
      pricedBy: 'ceník od',
      unpriceable: 'Tento interval nelze nacenit — uvnitř něj se mění cena.',
    },

    tariff: {
      title: 'Ceníky a poplatky',
      lede: 'Ceny platí od data dál, dokud je nevystřídá další ceník. Úprava jednoho ceníku nikdy nehne čísly před ním.',
      formTitle: 'Nový ceník',
      editTitle: 'Upravit ceník',
      effectiveFrom: 'Platí od',
      priceVT: 'Cena VT (Kč/MWh)',
      priceNT: 'Cena NT (Kč/MWh)',
      fee: 'Měsíční poplatky (Kč)',
      withVAT: 'Všechny tři částky zadávejte s DPH a včetně distribuce — použijí se přesně tak, jak je napíšete.',
      validity: (from: string, to: string) => `${from} – ${to}`,
      // The newest version has no end, and its end is DERIVED rather than stored
      // (D136) — so there is nothing to print on the right. "dosud" would be
      // wrong for the common case of a version dated in the future, which has not
      // been running at all; "od <datum>" is true whether it starts tomorrow or
      // started last year.
      validityOpen: (from: string) => `od ${from}`,
      coversDays: (days: string) => `platí pro ${days} tohoto období`,
      future: 'budoucí',
      futureNote: 'Ceník s budoucím datem je běžná věc — predikce ho začne používat okamžitě.',
      empty: 'Zatím žádný ceník',
      deleteConfirm: 'Smazat ceník?',
      save: 'Uložit ceník',
    },

    advance: {
      title: 'Předpis záloh',
      lede: 'Záloha platí od data dál, stejně jako ceník. Když jste některý měsíc zaplatili jinak, zapište platbu — ta má přednost.',
      formTitle: 'Nový předpis záloh',
      editTitle: 'Upravit předpis záloh',
      effectiveFrom: 'Platí od',
      amount: 'Záloha (Kč/měsíc)',
      dueDay: 'Den splatnosti',
      dueDayHint: 'Číslo 1–31. V kratších měsících se posune na poslední den — 31. v únoru vyjde na 28.',
      // The clamped date is already printed immediately before this note, so
      // repeating it says nothing. What the reader needs is WHY it moved: the
      // předpis names a day this particular month does not have.
      clamped: 'předpis má pozdější den, tento měsíc tolik dní nemá',
      empty: 'Zatím žádný předpis záloh',
      save: 'Uložit předpis',
      deleteConfirm: 'Smazat předpis záloh?',
    },

    payment: {
      title: 'Zaplacené zálohy',
      lede: 'Nepovinné. Zapisujte jen měsíce, kdy jste zaplatili jinak, než říká předpis.',
      month: 'Měsíc',
      amount: 'Zaplaceno (Kč)',
      paidOn: 'Datum platby',
      empty: 'Žádné platby nejsou zapsané — počítá se předpis.',
      save: 'Uložit platbu',
      deleteConfirm: 'Smazat platbu?',
    },

    advances: {
      title: 'Zálohy',
      // NOT "Zaplaceno zatím". The figure behind this label is due_so_far_haler —
      // the sum of counted months whose den splatnosti has passed, taken from the
      // PŘEDPIS unless a platba was recorded. Calling it "zaplaceno" would assert
      // money left the account for a měsíc nobody confirmed paying.
      dueSoFar: 'Splatné zatím',
      dueSoFarNote: 'Podle předpisu — zaplacené zálohy zapisujte v Cenících a poplatcích.',
      expectedTotal: 'Za celé období',
      monthsCounted: (months: string) => `${months} v tomto období`,
      showMonths: 'Zobrazit započtené měsíce',
      hideMonths: 'Skrýt započtené měsíce',
      // D145 is the module's most surprising arithmetic, so the disclosure spells
      // out the rule rather than leaving it as folklore.
      countRule: 'Měsíc se počítá, pokud období obsahuje jeho první den. Roční období proto vyjde vždy na 12 měsíců, ať začíná kterýkoli den.',
      sourcePayment: 'zaplaceno',
      sourceSchedule: 'z předpisu',
      sourceNone: 'bez předpisu',
      due: 'splatnost',
      duePassed: 'už proběhla',
      recommendedVs: (recommended: string, current: string) =>
        `doporučeno ${recommended} · nyní ${current}`,
      recommendedNote: 'Záloha, při které by období skončilo na nule.',
    },

    period: {
      title: 'Zúčtovací období',
      formTitle: 'Nové zúčtovací období',
      editTitle: 'Upravit období',
      startsOn: 'Začátek',
      endsOn: 'Konec',
      endsOnHint: 'Předvyplní se na rok minus den. Dokud ho dodavatel nepotvrdí, zůstává předpokládaný.',
      confirmed: 'Konec potvrzený dodavatelem',
      none: 'Žádné zúčtovací období',
      noneHint: 'Elektřina se počítá po zúčtovacích obdobích — jedno založte a doplňte odečet k jeho prvnímu dni.',
      create: 'Založit období',
      save: 'Uložit období',
      deleteConfirm: 'Smazat období?',
      invoiceTitle: 'Vyúčtování od dodavatele',
      invoiceHint: 'Nepovinné, vyplňte, až dorazí. Nic se tím nezamyká — období zůstává upravitelné.',
      invoiceTotal: 'Vyúčtovaná částka (Kč)',
      invoiceBalance: 'Nedoplatek (−) / přeplatek (+) (Kč)',
      invoiceVT: 'Konečný stav VT (kWh)',
      invoiceNT: 'Konečný stav NT (kWh)',
      invoiceAt: 'Datum vyúčtování',
      // Two lines, because a difference in kWh and a difference in Kč mean
      // completely different things.
      comparisonKc: (computed: string, invoiced: string, diff: string) =>
        `spočteno ${computed} · vyúčtováno ${invoiced} · rozdíl ${diff}`,
      comparisonKwh: (computed: string, invoiced: string, diff: string) =>
        `naše kWh ${computed} · jejich ${invoiced} · rozdíl ${diff}`,
      comparisonHint: 'Rozdíl v kWh znamená jiný odečet, rozdíl jen v Kč jinou cenu.',
      // The same panel BEFORE the uzavírací odečet exists. Our side is still part
      // predikce, so the difference is mostly our own projection — attributing it
      // to their odečet would be false, and the hedge has to say so plainly.
      comparisonForecast: 'Naše strana je zatím predikce — chybí uzavírací odečet.',
      comparisonForecastHint:
        'Dokud odečet k poslednímu dni období nezapíšete, je rozdíl hlavně naše predikce, ne nesouhlas s dodavatelem.',
    },

    history: {
      title: 'Historie a grafy',
      empty: 'Historie zatím není',
      emptyHint: 'Grafy se objeví, jakmile budou zapsané aspoň dva odečty.',
      consumption: 'Spotřeba po měsících',
      cost: 'Náklady po měsících',
      // The approximation caveat, carried by the marks plus ONE footnote — never
      // by a warning banner over the chart.
      approximate: 'přibližné',
      approxNote: 'Spotřeba se mezi odečty rozpočítává rovnoměrně na dny, takže sloupce kWh jsou přibližné. Koruny jsou přesné — počítají se z opravdových odečtů a jen se rozdělují mezi měsíce.',
      pastPeriods: 'Uzavřená období',
    },

    // Form verbs. Module-scoped rather than added to `common`, which several
    // other screens read and which is not this module's to reshape.
    form: {
      cancel: 'Zrušit',
      saving: 'Ukládám…',
      save: 'Uložit',
      saveFailed: 'Uložení se nezdařilo',
      delete: 'Smazat',
      edit: 'Upravit',
      add: 'Přidat',
      optional: 'nepovinné',
      loadMore: 'Načíst starší odečty',
    },

    // Errors the user can actually act on.
    error: {
      loadFailed: 'Nepodařilo se načíst',
      loadFailedHint: 'Nic se neztratilo — všechno se počítá až při čtení.',
      retry: 'Zkusit znovu',
    },
  },

  // ---- v9: Soukromé položky a Úložiště ----
  //
  // ⚠ The vocabulary below is FIXED by PRD §V9-7 (D201) and must be used verbatim.
  // It is not translated, shortened, or replaced with a synonym: Poznámky ·
  // Soukromé poznámky · Dokumenty · Soukromé dokumenty · Viditelnost · Sdílené ·
  // Soukromé · Publikovat do sdílených · Vlastník · Úložiště · Databáze ·
  // Objektové úložiště (R2) · Nezařazené · Zálohovací bucket · Varovný práh ·
  // Soukromé položky · Trvale smazat.
  //
  // ⚠ AND NO STRING HERE MAY IMPLY ENCRYPTION. Private means access-controlled:
  // an admin with the database file or the R2 credentials can read anything, and
  // an admin can hard-delete a private item without being able to read it. Copy
  // promising secrecy beyond that is a bug, not a flourish.
  privacy: {
    switcherLabel: 'Kořen',
    sharedNotes: 'Poznámky',
    privateNotes: 'Soukromé poznámky',
    sharedDocuments: 'Dokumenty',
    privateDocuments: 'Soukromé dokumenty',
    visibility: 'Viditelnost',
    shared: 'Sdílené',
    private: 'Soukromé',
    /** The row-level mark. Says whose, not just that it is locked. */
    privateShort: 'Soukromé — jen ty',
    owner: 'Vlastník',

    // The empty private root is the ONLY place the feature gets to say what it is
    // for, so it says it plainly and without overpromising.
    /** Page subtitle in the private root — says who can see it, and no more. */
    subtitleNotes: 'Jen pro tebe — nikdo jiný je nevidí',
    subtitleDocuments: 'Jen pro tebe — nikdo jiný je nevidí',
    emptyNotesTitle: 'Tvoje soukromé poznámky',
    emptyNotesBody:
      'Sem si můžeš psát, co nepatří celé domácnosti. Nikdo jiný to neuvidí — ani správce. Kdykoli později můžeš jednotlivou poznámku publikovat do sdílených.',
    emptyDocumentsTitle: 'Tvoje soukromé dokumenty',
    emptyDocumentsBody:
      'Sem můžeš nahrát soubory, které nepatří celé domácnosti. Nikdo jiný je neuvidí — ani správce. Jednotlivý dokument můžeš kdykoli publikovat do sdílených.',

    // Scoped search (D184). The tree being searched is named in the placeholder
    // AND in the empty state, so nobody concludes a note has vanished because
    // they searched from the wrong root.
    searchSharedNotes: 'Hledat ve sdílených poznámkách…',
    searchPrivateNotes: 'Hledat v soukromých poznámkách…',
    searchSharedDocuments: 'Hledat ve sdílených dokumentech…',
    searchPrivateDocuments: 'Hledat v soukromých dokumentech…',
    searchedShared: 'Hledáno ve sdílených',
    searchedPrivate: 'Hledáno v soukromých',

    // Publish — the one irreversible action in either module (D182).
    //
    // ⚠ The weight is carried by the SENTENCE, not by colour or an icon: nothing
    // is being deleted, so the confirm button is `accent`, not `danger`. And
    // there is no undo toast, because there would be nothing to undo.
    publish: 'Publikovat do sdílených',
    publishNoteTitle: 'Publikovat poznámku do sdílených?',
    publishDocumentTitle: 'Publikovat dokument do sdílených?',
    publishFolderTitle: 'Publikovat složku do sdílených?',
    publishBody:
      'Uvidí to celá domácnost. Zpátky to nejde — kdybys to chtěl(a) zase jen pro sebe, musíš to nahrát znovu a sdílenou kopii smazat.',
    /** Folder variant: "publikovat složku" reads much smaller than what it does. */
    /**
     * ⚠ BOTH counts arrive ALREADY PLURALISED ("3 položky", "1 podsložka").
     * `folders` used to be a RAW number beside a hardcoded plural locative, so a
     * folder holding exactly one subfolder read "ve 1 podsložkách". This is the
     * one confirmation in the module that cannot be undone, so the copy is where
     * the weight sits.
     *
     * The locative phrasing was dropped rather than pluralised: Czech picks "v"
     * or "ve" from how the FOLLOWING NUMERAL is pronounced (v 1, ve 2, ve 3, v 5),
     * which no plural triple can express. A coordinated nominative pair needs no
     * preposition and reads the same.
     */
    publishFolderCount: (items: string, folders: string) =>
      folders
        ? `Zveřejní se celý obsah složky — ${items} a ${folders}.`
        : `Zveřejní se celý obsah složky — ${items}.`,
    /**
     * The count above walks the LIVE tree (fetched without archived items), but
     * the backend publishes archived descendants too — this sentence carries
     * what the number cannot.
     */
    publishFolderArchivedNote: 'Zveřejní se i případné archivované položky ve složce.',
    publishConfirm: 'Publikovat',
    published: 'Publikováno do sdílených',
    publishError: 'Publikování se nepodařilo',
    /** After a publish, before the lock disappears — a beat of feedback (D183). */
    nowShared: '✓ teď sdílené',

    // Pins (D183). "pro všechny" is unavailable-and-EXPLAINED, never silently
    // absent: hiding it would leave the member wondering what they did wrong.
    householdPinUnavailable: 'Soukromou položku nelze připnout pro všechny — ostatní ji nevidí.',

    // ⚠ THERE IS NO `crossScopeMove` STRING HERE, and the absence is the accurate
    // state. The backend refuses a cross-scope move with a 422 (D186), but the move
    // dialogs only ever offer targets from the tree the user is standing in, so
    // nothing in the UI can produce one — the copy that was written for it sat
    // unreferenced, reading to the next editor of this file like a wired feature.
    // Wire it back the day a move dialog can span both roots. The same went for
    // `publishSlugNote` and `publishing`, which no publish path ever rendered.
  },

  storage: {
    title: 'Úložiště',
    subtitle: 'Kolik místa co zabírá — v databázi a v objektovém úložišti.',

    database: 'Databáze',
    byModule: 'Podle modulu',
    objectStorage: 'Objektové úložiště (R2)',
    total: 'Celkem',
    wal: 'WAL',
    free: 'Volné místo v souboru',
    freeHint: 'Kolik by uvolnil VACUUM. Není to ztracené místo.',
    rows: 'Řádky',
    size: 'Velikost',
    indexes: 'Indexy',
    // ⚠ Says out loud what the module figure is made of. It is rows PLUS indexes,
    // and an FTS5 index routinely outweighs the table it indexes — so without this
    // sentence the per-table column looks like it does not add up to the total
    // beside it, which on this page reads as a measurement bug rather than as a
    // column the reader had not been told about.
    moduleTotalHint: 'Součet u modulu zahrnuje data i indexy.',
    // A virtual (FTS5) table's own line. It owns no pages and is not counted:
    // everything it costs sits in the four shadow tables listed beside it, so the
    // row says WHY it is zero rather than showing a bare 0 B that reads as an empty
    // index — or the *nezměřeno* it used to show, which read as a measurement that
    // failed on a page where that is a signal to act on.
    virtualTable: 'virtuální — data v tabulkách _data/_idx níže',

    // The three usage buckets.
    kindShared: 'Sdílené',
    kindPrivate: 'Soukromé',
    kindUnattributed: 'Nezařazené',
    // ⚠ `Nezařazené` is an ORDINARY ROW, not an error and not padding. Without
    // this sentence the number is meaningless and mildly alarming.
    unattributedHint:
      'Objekty, ke kterým už nepatří žádná živá položka — zbytky po nahrávání a mazání. Uklízí je průběžně zrcadlicí úloha; není to chyba.',

    // Unmeasured ≠ zero (D193).
    dbstatMissingTitle: 'Velikosti tabulek nejsou k dispozici',
    dbstatMissingBody:
      'Tahle verze SQLite neumí `dbstat`, takže se u tabulek zobrazují jen počty řádků. Celková velikost databáze je přesná. Nic se neodhaduje.',

    blobsDownTitle: 'Objektové úložiště je nedostupné',
    blobsDownBody: 'Čísla o databázi platí. Zkus to za chvíli znovu.',

    // The two lines that belong to nobody. They sit OUTSIDE the per-module
    // breakdown (D214/D205), or the page reads as if its own arithmetic were
    // broken.
    outsideBreakdown: 'Mimo rozpad podle modulů',
    outsideBreakdownHint:
      'Odvozené kopie celé databáze, ne data jednotlivých modulů. Do součtů výše se nezapočítávají.',
    replica: 'Litestream replika',
    // ⚠ Declined, not unimplemented (PRD §V9-12). The copy says what it is rather
    // than pretending the feature is coming.
    replicaOff: 'Nesleduje se',
    replicaOffHint:
      'Aplikace záměrně nemá přístupové údaje k záloze databáze, takže její velikost odsud nevidí. Stav replikace se zjišťuje na serveru.',
    backup: 'Zálohovací bucket',
    backupOff: 'Není nastavený',

    // The threshold (D196).
    overThreshold: 'Nad prahem',
    thresholdLabel: (mb: number) => `varovný práh ${mb} MB`,
    // ⚠ Says outright that nothing is blocked. Nobody has done anything wrong.
    thresholdBody: 'Nic se tím neblokuje — žádná kvóta, žádné odmítnuté nahrávání.',
    largestContributors: 'Největší podíl',

    generatedAt: 'Spočítáno',
    cached: 'z mezipaměti',
    refresh: 'Přepočítat',
    /** ⚠ Says the numbers on screen are the OLD ones — silence would let them be
     *  read as freshly recomputed, which is the one mistake this page must not
     *  invite. */
    refreshError: 'Přepočet se nepodařil — čísla níže jsou stále ta předchozí',
  },

  privateItems: {
    title: 'Soukromé položky',
    // The screen states its own discomfort rather than smoothing it away.
    subtitle:
      'Soukromé položky všech členů — identifikátor, vlastník, druh a velikost. Nikdy název, jméno souboru ani náhled.',
    audited: 'Otevření tohoto seznamu se zapisuje do Logu.',

    // ⚠ The empty state is a DESIGNED SCREEN, not a dash (D215). The tab is
    // present whether or not anything is listed, because hiding it would hide the
    // SCREEN, not the CAPABILITY — an admin can permanently delete another
    // member's private item either way, and a power that exists but is invisible
    // is worse than one that is stated.
    emptyTitle: 'Zatím tu nic není',
    emptyBody:
      'Nikdo nemá žádné soukromé položky. Až je mít bude, uvidíš tady jejich identifikátor, vlastníka, druh a velikost — nikdy obsah. Tahle stránka existuje proto, že správce může soukromou položku trvale smazat, aniž by ji směl otevřít.',

    colId: 'Identifikátor',
    colOwner: 'Vlastník',
    colKind: 'Druh',
    colSize: 'Velikost',
    colCreated: 'Vytvořeno',

    kindNote: 'Poznámka',
    kindDocument: 'Dokument',
    kindNoteFolder: 'Složka poznámek',
    kindDocumentFolder: 'Složka dokumentů',
    kindNoteImage: 'Obrázek v poznámce',
    // ⚠ Images have no delete route and should not: an image belongs to its note
    // and jde s ní (D204/D212). The screen says so rather than offering a button
    // that 405s.
    imageNotDeletable: 'Maže se s poznámkou, ke které patří.',

    sortRecent: 'Nejnovější',
    sortSize: 'Největší',
    // ⚠ `size` is single-page by design — a keyset cursor is an id, and an id does
    // not locate a position in a size ordering. The total below still covers
    // everything, so the figure the screen acts on stays complete.
    /** ⚠ Describes the MODE, not the list. It shows whenever `size` is selected —
     *  including for a list that fits on one page — so it must not claim the rows
     *  were cut short. What is always true: this ordering does not page, and the
     *  total below covers every matching item either way. */
    sortSizeTruncated: 'Řazení podle velikosti se nestránkuje. Celkový součet platí pro všechny položky.',
    filterAll: 'Vše',
    totalBytes: 'Celkem',
    /** Prefixes the footer count while more pages exist: the byte total beside it
     *  covers ALL matching items, so an unqualified loaded-rows count would read
     *  as the complete inventory. */
    shownCount: 'Zobrazeno',
    /** `recent` IS pageable — the cursor is an id and ids sort chronologically. */
    loadMore: 'Načíst další',

    purge: 'Trvale smazat',
    purgeTitle: 'Trvale smazat soukromou položku?',
    purgeBody:
      'Smaže se řádek i uložené soubory. Zpátky to nejde a obsah si nikdo nepřečte — ani ty.',
    purgeConfirmPrompt: 'Pro potvrzení opiš celý identifikátor:',
    purgeCascade: 'Smazat i celý obsah složky',
    purged: 'Trvale smazáno',
    purgeError: 'Smazání se nepodařilo',
  },
} as const
