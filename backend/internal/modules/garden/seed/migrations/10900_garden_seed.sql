-- garden BUILT-IN RULES — block 10900, PRODUCTION ONLY (PRD D115, HANDOFF-9 §13).
--
-- Applied by MigrationSourcesWithSeed() from cmd/home ONLY. testsupport.NewDB
-- migrates bootstrap.MigrationFS(), which excludes this file, so a fresh test
-- database has ZERO rules and a C1 fixture can only pass because of the rule the
-- test itself wrote. TestSeedExcludedFromTestDB asserts exactly that.
--
-- INSERT OR IGNORE against ux_garden_rules (scope, a_ref, b_ref), so re-running on
-- every boot is a no-op rather than a duplicate-key failure.
--
--
-- HOW BUILT-IN CROP PAIRS REFERENCE A CROP
--
-- A `plant_pair` rule's a_ref/b_ref normally hold PLANT IDS. These rows cannot:
-- they ship before any crop exists, and the household's crops are created later
-- by hand or by the LLM import. So a BUILT-IN plant_pair references a crop by its
-- Czech `name_cs`, which is unique among live crops and therefore a legitimate
-- natural key; the store resolves those names to ids when it loads rules for the
-- check, and silently drops any that name a crop this garden does not grow.
--
-- That last part is the useful half: a rule about celer is simply inert until
-- somebody plants celer, so seeding sixty pairs costs nothing in a garden that
-- grows twelve crops. User-created rules keep using ids, as the contract says.
--
--
-- ABOUT `source`
--
-- Companion-planting literature contradicts itself, freely and confidently. The
-- `source` column exists so agronomy and folklore stay tellable apart by looking
-- (D130), and these values name the KIND of claim rather than inventing a
-- citation:
--
--   agronomie — sdílení chorob   a documented shared pathogen or pest
--   agronomie — alelopatie       a documented growth-inhibiting chemical effect
--   agronomie — fixace dusíku    nitrogen fixation, which is simply true
--   agronomie — konkurence       competition for light, water or root space
--   agronomie — křížení          the two will cross-pollinate if you save seed
--   tradice                      long-standing companion-planting practice with
--                                weak or contested evidence
--
-- Anything marked `tradice` is there because the household may want it, not
-- because it is established. All of it can be disabled — and none of it deleted
-- (D130), so you can always see what you did not type.

-- +goose Up

-- ===================== family break years (succession) =====================
--
-- a_ref = b_ref = the family: "this family must not follow itself within N
-- seasons". Read by Effective.RotationBreak as the middle of three levels — the
-- crop's own value wins, this is the family default, and garden_settings supplies
-- the final fallback.

-- +goose StatementBegin
INSERT OR IGNORE INTO garden_rules
    (id, scope, a_ref, b_ref, verdict, severity, min_years_gap, reason_cs, source, is_builtin, created_at, updated_at)
VALUES
    ('01920000-7000-7000-8000-000000000001','succession','brassicaceae','brassicaceae','bad','error',4,'Brukvovité trpí nádorovitostí, která v půdě vydrží roky. Doporučená pauza jsou čtyři sezóny.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000002','succession','solanaceae','solanaceae','bad','error',4,'Lilkovité si v půdě předávají plíseň bramborovou a háďátka. Čtyři roky pauzy jsou minimum.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000003','succession','apiaceae','apiaceae','bad','warn',3,'Miříkovité po sobě přitahují vrtuli mrkvovou a padlí. Tři roky pauzy stačí.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000004','succession','fabaceae','fabaceae','bad','warn',2,'Bobovité si předávají fusariózu, ale půdu naopak obohacují. Dva roky pauzy jsou dost.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000005','succession','cucurbitaceae','cucurbitaceae','bad','warn',3,'Tykvovité po sobě zhoršují padlí a fuzáriové vadnutí. Tři roky pauzy.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000006','succession','amaryllidaceae','amaryllidaceae','bad','error',3,'Cibuloviny trpí bílou hnilobou, jejíž sklerocia přežívají v půdě velmi dlouho. Tři roky jsou nutné minimum.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000007','succession','amaranthaceae','amaranthaceae','bad','warn',3,'Laskavcovité (řepa, špenát, mangold) si předávají skvrnitost listů a háďátko řepné.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000008','succession','asteraceae','asteraceae','bad','warn',2,'Hvězdnicovité po sobě zvyšují tlak plísně salátové. Dva roky pauzy.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000009','succession','poaceae','poaceae','bad','info',2,'Obiloviny a kukuřice po sobě vyčerpávají dusík a přitahují bázlivce.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000000a','succession','lamiaceae','lamiaceae','bad','info',2,'Hluchavkovité bylinky snesou i kratší pauzu, dva roky jsou bezpečné.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000000b','succession','polygonaceae','polygonaceae','bad','info',2,'Rdesnovité (šťovík, reveň) po sobě vyčerpávají půdu.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000000c','succession','valerianaceae','valerianaceae','bad','info',2,'Kozlíkovité (polníček) po sobě zvyšují tlak plísní.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000000d','succession','rosaceae','rosaceae','bad','warn',3,'Růžovité (jahody, ovocné dřeviny) trpí únavou půdy — nová výsadba na stejné místo se ujímá hůř.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000000e','succession','grossulariaceae','grossulariaceae','bad','info',2,'Meruzalkovité (rybíz, angrešt) po sobě rostou hůř kvůli únavě půdy.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000000f','succession','other','other','bad','info',2,'Bez určené čeledi platí opatrné dva roky pauzy.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z');
-- +goose StatementEnd

-- ============================ family pairings ============================
--
-- Deliberately few. A family_pair is a blunt instrument — it speaks for every
-- crop in both families at once — so only claims that really do hold at family
-- level belong here. Everything more specific is a plant_pair below, and an
-- explicit plant_pair always wins (FR-G13).

-- +goose StatementBegin
INSERT OR IGNORE INTO garden_rules
    (id, scope, a_ref, b_ref, verdict, severity, min_years_gap, reason_cs, source, is_builtin, created_at, updated_at)
VALUES
    ('01920000-7000-7000-8000-000000000101','family_pair','amaryllidaceae','fabaceae','bad','warn',NULL,'Cibuloviny tlumí hlízkové bakterie luskovin, takže hrách a fazole vedle cibule a česneku hůř vážou dusík.','agronomie — alelopatie',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000102','family_pair','fabaceae','solanaceae','good','info',NULL,'Luskoviny obohatí půdu o dusík, který lilkovité v témže záhonu vítají.','agronomie — fixace dusíku',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000103','family_pair','amaryllidaceae','apiaceae','good','info',NULL,'Miříkovité a cibuloviny si navzájem pletou škůdce vůní — klasická dvojice mrkve a cibule platí i v širším měřítku.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z');
-- +goose StatementEnd

-- ====================== crop pairs that do NOT work ======================

-- +goose StatementBegin
INSERT OR IGNORE INTO garden_rules
    (id, scope, a_ref, b_ref, verdict, severity, min_years_gap, reason_cs, source, is_builtin, created_at, updated_at)
VALUES
    ('01920000-7000-7000-8000-000000000201','plant_pair','fenykl','rajče','bad','error',NULL,'Fenykl vylučuje látky, které brzdí růst většiny sousedů — rajčatům škodí nejvíc. Sázejte ho stranou od všeho ostatního.','agronomie — alelopatie',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000202','plant_pair','fazole','fenykl','bad','error',NULL,'Fenykl potlačuje klíčení i růst fazolí.','agronomie — alelopatie',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000203','plant_pair','fenykl','kopr','bad','warn',NULL,'Fenykl a kopr se navzájem zkříží a semena pak nejsou k ničemu.','agronomie — křížení',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000204','plant_pair','brambory','rajče','bad','error',NULL,'Obojí jsou lilkovité a plíseň bramborová přeskočí z jednoho na druhé během jediného vlhkého týdne.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000205','plant_pair','brambory','okurka','bad','warn',NULL,'Okurky jsou na plíseň z bramborové natě obzvlášť citlivé.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000206','plant_pair','brambory','dýně','bad','warn',NULL,'Dýně a brambory se dělí o plísně a obojí potřebuje hodně místa i živin.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000207','plant_pair','brambory','slunečnice','bad','warn',NULL,'Slunečnice brambory přerůstá, stíní jim a vyčerpává jim vodu.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000208','plant_pair','cibule','hrách','bad','warn',NULL,'Hrách vedle cibule hůř váže dusík a zůstává drobný.','agronomie — alelopatie',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000209','plant_pair','cibule','fazole','bad','warn',NULL,'Fazole vedle cibule zaostávají v růstu.','agronomie — alelopatie',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000020a','plant_pair','česnek','fazole','bad','warn',NULL,'Česnek tlumí hlízkové bakterie na kořenech fazolí.','agronomie — alelopatie',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000020b','plant_pair','česnek','hrách','bad','warn',NULL,'Hrách vedle česneku roste pomaleji a hůř nasazuje lusky.','agronomie — alelopatie',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000020c','plant_pair','kopr','mrkev','bad','warn',NULL,'Kopr a mrkev jsou příbuzné, kříží se a sdílejí vrtuli mrkvovou. Kvetoucí kopr navíc mrkev přerůstá.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000020d','plant_pair','kukuřice','rajče','bad','warn',NULL,'Kukuřice a rajčata živí stejnou housenku (můru), takže vedle sebe si ji předávají.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000020e','plant_pair','okurka','šalvěj','bad','info',NULL,'Silně aromatická šalvěj okurkám podle zkušeností nesvědčí.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000020f','plant_pair','jahody','zelí','bad','info',NULL,'Zelí jahody přerůstá a stíní jim; tradičně se nedoporučuje.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000210','plant_pair','celer','mrkev','bad','info',NULL,'Obojí jsou miříkovité a lákají stejného škůdce — vrtuli mrkvovou.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000211','plant_pair','mrkev','petržel','bad','info',NULL,'Petržel a mrkev jsou příbuzné, sdílejí škůdce i choroby a soupeří o stejnou hloubku půdy.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000212','plant_pair','fenykl','paprika','bad','warn',NULL,'Fenykl brzdí růst paprik stejně jako rajčat.','agronomie — alelopatie',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000213','plant_pair','brokolice','rajče','bad','info',NULL,'Rajčata jsou velmi náročná na živiny a brokolici je v jednom záhonu odčerpají.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000214','plant_pair','kedluben','rajče','bad','info',NULL,'Kedluben vedle rajčat zůstává malý — rajčata si vezmou živiny i světlo.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000215','plant_pair','jahody','květák','bad','info',NULL,'Květák jahodám stíní a odčerpává jim živiny.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000216','plant_pair','brambory','lilek','bad','error',NULL,'Lilek a brambory jsou lilkovité a mají společné všechny choroby i mandelinku.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000217','plant_pair','brambory','cuketa','bad','warn',NULL,'Cukety a brambory se dělí o plísně a obojí je náročné na živiny.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000218','plant_pair','hrách','pórek','bad','info',NULL,'Pórek jako cibulovina tlumí luskoviny podobně jako cibule.','agronomie — alelopatie',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z');
-- +goose StatementEnd

-- ======================== crop pairs that work ========================

-- +goose StatementBegin
INSERT OR IGNORE INTO garden_rules
    (id, scope, a_ref, b_ref, verdict, severity, min_years_gap, reason_cs, source, is_builtin, created_at, updated_at)
VALUES
    ('01920000-7000-7000-8000-000000000301','plant_pair','cibule','mrkev','good','info',NULL,'Nejznámější dvojice na zahradě: vůně cibule mate vrtuli mrkvovou a vůně mrkve zase květilku cibulovou.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000302','plant_pair','mrkev','pórek','good','info',NULL,'Pórek a mrkev se navzájem chrání před svými škůdci — funguje to stejně jako s cibulí.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000303','plant_pair','bazalka','rajče','good','info',NULL,'Bazalka mezi rajčaty odpuzuje molice a mšice a nikomu nepřekáží — a sklízí se ve stejnou dobu.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000304','plant_pair','měsíček','rajče','good','info',NULL,'Měsíček láká pestřenky a slunéčka, která si poradí s mšicemi na rajčatech.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000305','plant_pair','petržel','rajče','good','info',NULL,'Petržel pod rajčaty využije stín a přiláká užitečný hmyz.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000306','plant_pair','kopr','okurka','good','info',NULL,'Kopr láká k okurkám opylovače i užitečný hmyz — a sklidíte ho právě ve chvíli, kdy nakládáte.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000307','plant_pair','okurka','slunečnice','good','info',NULL,'Slunečnice poslouží okurkám jako živá opora a mírné stínění.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000308','plant_pair','aksamitník','zelí','good','info',NULL,'Aksamitník potlačuje háďátka v půdě a odvádí pozornost bělásků od zelí.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000309','plant_pair','kopr','zelí','good','info',NULL,'Kopr u zelí láká parazitické vosičky, které hubí housenky bělásků.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000030a','plant_pair','celer','zelí','good','info',NULL,'Celer mezi zelím odpuzuje bělásky a využije místo mezi hlávkami.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000030b','plant_pair','fazole','kukuřice','good','info',NULL,'Klasická trojice: kukuřice dá fazolím oporu, fazole vrátí půdě dusík.','agronomie — fixace dusíku',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000030c','plant_pair','dýně','fazole','good','info',NULL,'Dýně zastíní půdu a udrží vláhu, fazole ji obohatí o dusík.','agronomie — fixace dusíku',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000030d','plant_pair','dýně','kukuřice','good','info',NULL,'Dýňové listy pod kukuřicí fungují jako živý mulč — méně plevele, méně zálivky.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000030e','plant_pair','ředkvička','salát','good','info',NULL,'Ředkvička je z půdy pryč dřív, než se salát rozroste — dva výnosy z jednoho místa.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000030f','plant_pair','česnek','jahody','good','info',NULL,'Česnek mezi jahodami omezuje šedou hnilobu a odpuzuje škůdce.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000310','plant_pair','jahody','špenát','good','info',NULL,'Špenát mezi mladými jahodami zastíní půdu a sklidí se dřív, než si jahody řeknou o místo.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000311','plant_pair','mrkev','salát','good','info',NULL,'Salát vyplní řádky, než mrkev vzejde, a udrží půdu bez plevele.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000312','plant_pair','mrkev','ředkvička','good','info',NULL,'Ředkvička označí pomalu klíčící řádek mrkve a stihne se sklidit včas.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000313','plant_pair','brambory','fazole','good','info',NULL,'Fazole mezi bramborami dodají dusík a odpuzují mandelinku.','agronomie — fixace dusíku',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000314','plant_pair','brambory','hořčice','good','info',NULL,'Hořčice po bramborách omezuje háďátka a rychle zakryje uvolněný záhon.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000315','plant_pair','bazalka','paprika','good','info',NULL,'Bazalka u paprik odpuzuje mšice a snáší stejné teplo i zálivku.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000316','plant_pair','cuketa','měsíček','good','info',NULL,'Měsíček u cuket přitáhne opylovače, na kterých úroda přímo závisí.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000317','plant_pair','ředkvička','špenát','good','info',NULL,'Obojí je z půdy rychle pryč a dohromady vyplní jarní záhon, než přijde hlavní plodina.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000318','plant_pair','hrách','mrkev','good','info',NULL,'Hrách zanechá v půdě dusík, který mrkev v druhé polovině sezóny ocení.','agronomie — fixace dusíku',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000319','plant_pair','hrách','ředkvička','good','info',NULL,'Ředkvička využije místo mezi řádky hrachu, než se hrách rozroste.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000031a','plant_pair','celer','pórek','good','info',NULL,'Celer a pórek mají podobné nároky a navzájem si pletou škůdce.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000031b','plant_pair','pažitka','rajče','good','info',NULL,'Pažitka kolem rajčat odpuzuje mšice a vydrží v záhonu roky.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000031c','plant_pair','aksamitník','rajče','good','info',NULL,'Aksamitník potlačuje háďátka, na která jsou rajčata citlivá.','agronomie — sdílení chorob',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000031d','plant_pair','brokolice','salát','good','info',NULL,'Salát vyplní místo mezi mladou brokolicí a sklidí se dřív, než ho zastíní.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000031e','plant_pair','kedluben','salát','good','info',NULL,'Kedlubny a salát mají stejně krátkou dobu do sklizně a nepřekážejí si.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-00000000031f','plant_pair','cibule','řepa','good','info',NULL,'Řepa a cibule se dělí o záhon bez konkurence — kořeny rostou v jiné hloubce.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000320','plant_pair','meduňka','zelí','good','info',NULL,'Aromatická meduňka mate bělásky, kteří zelí hledají po vůni.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000321','plant_pair','tymián','zelí','good','info',NULL,'Tymián u zelí odpuzuje housenky a snese sucho na kraji záhonu.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000322','plant_pair','kapusta','ředkvička','good','info',NULL,'Ředkvička odvede pozornost dřepčíků od mladé kapusty.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000323','plant_pair','fazole','okurka','good','info',NULL,'Fazole obohatí půdu o dusík, který okurky spotřebují ve velkém.','agronomie — fixace dusíku',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000324','plant_pair','hrách','salát','good','info',NULL,'Salát v polostínu hrachu déle vydrží a nevybíhá do květu.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000325','plant_pair','česnek','rajče','good','info',NULL,'Česnek mezi rajčaty omezuje plísně a mšice.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000326','plant_pair','česnek','mrkev','good','info',NULL,'Česnek mate vrtuli mrkvovou stejně jako cibule.','tradice',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000327','plant_pair','špenát','zelí','good','info',NULL,'Špenát zakryje půdu mezi mladým zelím a sklidí se dřív, než si zelí řekne o místo.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z'),
    ('01920000-7000-7000-8000-000000000328','plant_pair','polníček','ředkvička','good','info',NULL,'Polníček i ředkvička zvládnou chladné okraje sezóny a dohromady vyplní záhon na jaře i na podzim.','agronomie — konkurence',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00Z');
-- +goose StatementEnd

-- +goose Down

-- Built-in rules cannot be deleted through the API (D130), but a migration
-- rollback is a different thing: it removes exactly what this file inserted and
-- leaves anything the household typed alone.
-- +goose StatementBegin
DELETE FROM garden_rules WHERE is_builtin = 1;
-- +goose StatementEnd
