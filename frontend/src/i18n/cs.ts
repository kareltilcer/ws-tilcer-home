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

  // ---- v5: app-wide offline ----
  offline: {
    banner: 'Jste offline — zobrazená data mohou být starší',
    bannerSub: 'Změny nelze uložit offline. Náhledy dokumentů a přihlášení vyžadují připojení.',
    writeBlocked: 'Změny nelze uložit offline',
    needsConnection: 'Vyžaduje připojení',
  },
} as const
