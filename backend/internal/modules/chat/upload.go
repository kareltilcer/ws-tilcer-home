package chat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/audit"
	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/httpx"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/reqctx"
)

// Attachment upload (FR-V10-6, D224/D227/D228).
//
// ⚠ ONE REQUEST, NEVER AN UPLOAD-THEN-REFERENCE PAIR (D224). A two-step flow
// orphans an object every time the second step does not happen, and chat has no
// reconciliation pass to find one — `documents` has a mirror job that sweeps its
// prefix; chat deliberately has neither a mirror nor a sweeper (D229).
//
// ⚠ THE PER-FILE CAP IS DOKUMENTY'S, AND THE REASON IS NOT THRIFT (D228). A file
// above Dokumenty's cap could never be MOVED into Dokumenty, so the clean-up page's
// headline action would fail on exactly the files heavy enough to have caused the
// overrun. One cap keeps every attachment movable by construction.
//
// THE ORDERING, which is the same one FR-DOC1 fixed in v4 and for the same reason:
//
//	1. stage every part through a hard cap into a temp file, hashing as it streams.
//	   Over the cap → 413 naming the limit, with nothing written anywhere.
//	2. sniff the type from the bytes and classify it (D48/D227). Images get their
//	   intrinsic dimensions read and a thumb.webp encoded — both non-fatal.
//	3. PUT every object. A storage failure → 502 and NO database row.
//	4. only then, in ONE transaction, the message row, its attachment rows and one
//	   audit event per attachment.
//	5. on a transaction failure, best-effort delete the objects just written.
//
// A crash between 3 and 4 leaves objects with no rows — reported by StorageBlobs as
// `unattributed`, never auto-cleaned, and harmless. The reverse (rows with no
// bytes) is a broken thread, which is why the objects go first.

// maxFilesPerMessage is D224's ten.
const maxFilesPerMessage = 10

// maxUploadParts bounds the multipart loop. Without it an authenticated client
// could hold the request open indefinitely streaming tiny fields that never become
// a file part — the same guard documents/http.go carries.
const maxUploadParts = 64

// metaFieldLimit caps a non-file multipart field. `body` is 8 000 runes, so 64 KB
// is generous for any encoding of it and small enough that a field cannot be a
// smuggled upload.
const metaFieldLimit = 64 << 10

// stagedFile is one part, already on local disk with everything known about it.
type stagedFile struct {
	id          string
	filename    string
	contentType string
	kind        string
	checksum    string
	size        int64
	path        string
	// thumbPath is "" when there is no thumbnail — a video, a file, or an image
	// whose encoding failed. ⚠ A missing thumbnail is NOT an error state: the
	// bubble falls back to the full image, which is what it would show anyway.
	thumbPath string
	// thumbStored is set by putStaged when the thumbnail actually reached the
	// bucket.
	//
	// ⚠ IT IS A SECOND FLAG BECAUSE thumbPath ANSWERS A DIFFERENT QUESTION —
	// "was it encoded", not "was it stored" — and keying the row on the first one
	// made the database claim a thumbnail that is not in R2. putStaged deliberately
	// swallows a storage failure (a message must not fail because a derived object
	// did), so the row would then set thumbnail_key, every render of that image
	// would request /thumbnail, and the server would 404 it forever after resolving
	// a key to nothing.
	thumbStored   bool
	width, height int
	dir           string
}

func (f stagedFile) cleanup() {
	if f.dir != "" {
		_ = os.RemoveAll(f.dir)
	}
}

// SendMessageMultipart is the send path for a message carrying files.
//
// ⚠ MEMBERSHIP IS RESOLVED BEFORE A SINGLE BYTE IS STAGED, and then AGAIN inside
// the writing transaction. The first check is not the authorisation — the second
// one is, because only it is atomic with the write — but without it a non-member
// could spend 500 MB of the droplet's disk discovering they are not in the room.
// A cheap indexed read is the whole cost of closing that.
func (s *Service) SendMessageMultipart(ctx context.Context, conversationID string, mr *multipart.Reader) (Message, error) {
	actor := reqctx.ActorID(ctx)
	if actor == "" {
		return Message{}, httpx.ErrUnauthorized("")
	}
	if s.blob == nil {
		return Message{}, httpx.ErrInternal("chat attachment storage is not configured")
	}
	if _, err := s.store.memberScope(ctx, s.db, actor, conversationID); err != nil {
		return Message{}, mapScopeErr(err)
	}

	var (
		in    MessageCreate
		files []stagedFile
	)
	defer func() {
		for _, f := range files {
			f.cleanup()
		}
	}()

	for parts := 0; ; parts++ {
		if parts >= maxUploadParts {
			return Message{}, httpx.ErrUnprocessable("Zpráva má příliš mnoho částí.")
		}
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Message{}, httpx.ErrUnprocessable("Poškozený multipart požadavek: " + err.Error())
		}
		if part.FileName() == "" && part.FormName() != "files" {
			if err := readMetaField(part, &in); err != nil {
				_ = part.Close()
				return Message{}, err
			}
			_ = part.Close()
			continue
		}
		if len(files) >= maxFilesPerMessage {
			_ = part.Close()
			return Message{}, httpx.ErrUnprocessable(
				fmt.Sprintf("Ke zprávě lze připojit nejvýše %d souborů.", maxFilesPerMessage))
		}
		staged, err := s.stage(ctx, part)
		_ = part.Close()
		if err != nil {
			return Message{}, err
		}
		files = append(files, staged)
	}

	body, err := validateBody(in.Body)
	if err != nil {
		return Message{}, err
	}
	// ⚠ THE "BODY OR ATTACHMENT" INVARIANT, in the write path rather than as a table
	// CHECK (D224). chat_messages carries an explicit rowid alias for its FTS5 index
	// and must never be rebuilt, so it can never gain one — the v9 D179 precedent.
	if strings.TrimSpace(body) == "" && len(files) == 0 {
		return Message{}, httpx.ErrUnprocessable("Zpráva nesmí být prázdná.")
	}

	// 3. Objects first. Nothing is written to the database until every one is durable.
	written := make([]string, 0, len(files)*2)
	for i := range files {
		keys, err := s.putStaged(ctx, &files[i])
		written = append(written, keys...)
		if err != nil {
			s.purgeObjects(ctx, written)
			s.logger.Error("chat: storing an attachment failed — nothing committed",
				"attachment", files[i].id, "err", err)
			return Message{}, httpx.ErrBadGateway("Úložiště souborů není dostupné.")
		}
	}

	msg, err := s.commitMessage(ctx, conversationID, body, in.ReplyToID, files)
	if err != nil {
		// The rows never landed, so the objects are orphans. Delete them here rather
		// than leaving them to a sweeper chat does not have.
		s.purgeObjects(ctx, written)
		return Message{}, err
	}
	return msg, nil
}

// stage streams one part through the cap into a temp file and learns everything
// about it.
func (s *Service) stage(ctx context.Context, part *multipart.Part) (stagedFile, error) {
	filename := safeFilename(part.FileName())
	dir, err := os.MkdirTemp(s.upload.TempDir, "home-chat-*")
	if err != nil {
		return stagedFile{}, httpx.ErrInternal("cannot buffer the upload: " + err.Error())
	}
	f := stagedFile{id: idgen.New(), filename: filename, dir: dir}

	f.path = filepath.Join(dir, "original")
	dst, err := os.Create(f.path)
	if err != nil {
		f.cleanup()
		return stagedFile{}, httpx.ErrInternal("cannot buffer the upload: " + err.Error())
	}
	// 1. Stream with the cap. Reading max+1 bytes is how "over the cap" is detected
	// without trusting Content-Length, which a client can lie about or omit.
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(dst, hasher), io.LimitReader(part, s.upload.MaxBytes+1))
	closeErr := dst.Close()
	if copyErr != nil {
		f.cleanup()
		return stagedFile{}, httpx.ErrBadRequest("Nahrávání souboru selhalo: " + copyErr.Error())
	}
	if closeErr != nil {
		f.cleanup()
		return stagedFile{}, httpx.ErrInternal("cannot buffer the upload: " + closeErr.Error())
	}
	if size > s.upload.MaxBytes {
		f.cleanup()
		// ⚠ The limit is NAMED, in MB, because the composer refuses over-cap files
		// before uploading them and this message is what it echoes when it did not.
		return stagedFile{}, httpx.ErrTooLarge(
			fmt.Sprintf("Soubor „%s“ je větší než povolených %d MB.", filename, s.upload.MaxBytes>>20))
	}
	if size == 0 {
		f.cleanup()
		return stagedFile{}, httpx.ErrUnprocessable(fmt.Sprintf("Soubor „%s“ je prázdný.", filename))
	}
	f.size = size
	f.checksum = hex.EncodeToString(hasher.Sum(nil))

	// 2. Sniff from the bytes just stored — never the part's own Content-Type (D48).
	head, err := readHead(f.path)
	if err != nil {
		f.cleanup()
		return stagedFile{}, httpx.ErrInternal("cannot inspect the upload: " + err.Error())
	}
	f.contentType = sniffChatType(head, filename)
	f.kind = kindFor(f.contentType)

	if f.kind == kindImage {
		// Both halves are non-fatal, and they fail independently: a dimension read
		// that works and an encode that does not still stops the thread reflowing.
		if w, h, err := dimensionsOf(f.path); err == nil {
			f.width, f.height = w, h
		} else {
			s.logger.Warn("chat: could not read image dimensions", "attachment", f.id, "err", err)
		}
		if thumb, err := makeThumbnail(ctx, s.upload.Thumb, f.path, dir); err == nil {
			f.thumbPath = thumb
		} else {
			s.logger.Warn("chat: thumbnail generation failed — the bubble renders the original",
				"attachment", f.id, "err", err)
		}
	}
	return f, nil
}

// putStaged writes one staged file's objects, returning the keys it actually wrote
// so a later failure can undo them — and recording on `f` whether the THUMBNAIL got
// there, which is a different question from whether it was encoded.
func (s *Service) putStaged(ctx context.Context, f *stagedFile) ([]string, error) {
	written := []string{}
	body, err := os.Open(f.path)
	if err != nil {
		return written, err
	}
	key := originalKey(f.id)
	err = s.blob.Put(ctx, key, body, f.size, f.contentType)
	_ = body.Close()
	if err != nil {
		return written, err
	}
	written = append(written, key)
	if f.thumbPath == "" {
		return written, nil
	}
	info, err := os.Stat(f.thumbPath)
	if err != nil {
		// The thumbnail is optional; losing it here costs a fallback render, not a
		// message. The original is already durable.
		s.logger.Warn("chat: staged thumbnail vanished", "attachment", f.id, "err", err)
		return written, nil
	}
	thumb, err := os.Open(f.thumbPath)
	if err != nil {
		s.logger.Warn("chat: staged thumbnail unreadable", "attachment", f.id, "err", err)
		return written, nil
	}
	tk := thumbnailKey(f.id)
	err = s.blob.Put(ctx, tk, thumb, info.Size(), "image/webp")
	_ = thumb.Close()
	if err != nil {
		// Same reasoning: a message must not fail because a derived object did.
		s.logger.Warn("chat: storing a thumbnail failed", "attachment", f.id, "err", err)
		return written, nil
	}
	f.thumbStored = true
	return append(written, tk), nil
}

// commitMessage is SendMessage's transaction with the attachment rows folded in.
//
// It deliberately mirrors SendMessage step for step — the same in-transaction id
// mint, the same audience resolution, the same conversation bump — because the two
// differ only in what they carry, and a second ordering here would be a second
// answer to "when is a message's id decided".
func (s *Service) commitMessage(ctx context.Context, conversationID, body string, replyToID *string, files []stagedFile) (Message, error) {
	actor := reqctx.ActorID(ctx)
	labels, err := s.labels(ctx)
	if err != nil {
		return Message{}, err
	}
	var (
		id         string
		now        string
		prev       *string
		audience   []string
		recipients []string
		convName   string
		replyTo    *string
		sentScope  Scope
	)
	if replyToID != nil && strings.TrimSpace(*replyToID) != "" {
		replyTo = replyToID
	}
	err = appdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		sc, err := s.store.memberScope(ctx, tx, actor, conversationID)
		if err != nil {
			return err
		}
		id, now = idgen.New(), nowUTC()
		sentScope = sc
		if replyTo != nil {
			if _, err := s.store.MessageByID(ctx, tx, sc, *replyTo); err != nil {
				if errors.Is(err, errMessageNotFound) {
					return httpx.ErrUnprocessable("Odpovídat lze jen na zprávu z této konverzace.")
				}
				return err
			}
		}
		prev, err = s.store.InsertMessage(ctx, tx, id, sc.ConversationID, actor, body, replyTo, now)
		if err != nil {
			return err
		}
		convName, err = s.store.ConversationName(ctx, tx, sc.ConversationID)
		if err != nil {
			return err
		}
		for _, f := range files {
			row := attachmentRow{
				ID: f.id, MessageID: id, ConversationID: sc.ConversationID,
				Kind: f.kind, OriginalFilename: f.filename, ContentType: f.contentType,
				ByteSize: f.size, Checksum: f.checksum, StorageKey: originalKey(f.id),
				UploadedBy: actor, CreatedAt: now,
			}
			// ⚠ thumbStored, NOT thumbPath — the row must describe the BUCKET.
			if f.thumbStored {
				row.ThumbnailKey = sql.NullString{String: thumbnailKey(f.id), Valid: true}
			}
			if f.width > 0 && f.height > 0 {
				row.Width = sql.NullInt64{Int64: int64(f.width), Valid: true}
				row.Height = sql.NullInt64{Int64: int64(f.height), Valid: true}
			}
			if err := s.store.InsertAttachment(ctx, tx, row); err != nil {
				return err
			}
			// ⚠ ATTACHMENTS ARE AUDITED ALTHOUGH THE MESSAGE CARRYING THEM IS NOT,
			// and that asymmetry is deliberate rather than inconsistent (D231/§14).
			// The BYTES are what the two thresholds, the clean-up page and the
			// storage register exist for, and "who uploaded that 40 MB video, and
			// when" is the question the whole storage half of v10 answers. The event
			// carries the filename and the conversation name and no message text.
			if err := s.recordAttachment(ctx, tx, "attachment.uploaded", f.id,
				fmt.Sprintf("Nahrán soubor „%s“ do konverzace „%s“", f.filename, convName),
				[]audit.Change{
					{Field: "original_filename", New: audit.Ptr(f.filename)},
					{Field: "content_type", New: audit.Ptr(f.contentType)},
					{Field: "byte_size", New: audit.Ptr(fmt.Sprint(f.size))},
					{Field: "conversation", New: audit.Ptr(convName)},
				}); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE chat_conversations SET updated_at = ? WHERE id = ?`, now, sc.ConversationID); err != nil {
			return err
		}
		audience, err = s.store.MemberIDs(ctx, tx, sc.ConversationID)
		if err != nil {
			return err
		}
		recipients, err = s.store.PushRecipients(ctx, tx, sc.ConversationID, actor)
		return err
	})
	if err != nil {
		return Message{}, mapScopeErr(err)
	}

	rendered := Message{
		ID: id, ConversationID: conversationID, AuthorID: actor,
		AuthorLabel: label(labels, actor), Body: body,
		Attachments: make([]Attachment, 0, len(files)),
		// Never null (D174), the same as the text-only send one file over.
		Reactions: []Reaction{},
		CreatedAt: now,
	}
	for _, f := range files {
		a := Attachment{
			ID: f.id, Kind: f.kind, State: stateLive, OriginalFilename: f.filename,
			ContentType: f.contentType, ByteSize: f.size, HasThumbnail: f.thumbStored,
			UploadedBy: actor, CreatedAt: now,
		}
		if f.width > 0 && f.height > 0 {
			w, h := f.width, f.height
			a.Width, a.Height = &w, &h
		}
		rendered.Attachments = append(rendered.Attachments, a)
	}
	if replyTo != nil {
		if quote, qerr := s.store.Quote(ctx, s.db, sentScope, *replyTo, labels); qerr != nil {
			s.logger.Warn("chat: quote render after send", "err", qerr, "message", id)
		} else {
			rendered.ReplyTo = quote
		}
	}

	s.publishMessage(ctx, audience, rendered, prev)
	s.pushAfterSend(ctx, convName, recipients, rendered)
	return rendered, nil
}

// recordAttachment writes one of the three chat.attachment.* events.
//
// ⚠ IT IS A SEPARATE FUNCTION FROM Service.record BECAUSE THE ENTITY DIFFERS, not
// as a style choice: the Log's entity timeline is an exact-id match (§V9-12's D209),
// so an attachment event filed under `chat_conversation` would put a file's history
// on a room's timeline and make neither of them findable by its own id.
func (s *Service) recordAttachment(ctx context.Context, tx *sql.Tx, action, attachmentID, summary string, changes []audit.Change) error {
	return s.sink.Record(ctx, tx, audit.Event{
		Action:     action,
		EntityType: "chat_attachment",
		EntityID:   attachmentID,
		Summary:    summary,
		Changes:    changes,
	})
}

// purgeObjects best-effort deletes keys that should not have survived. Failures are
// logged and swallowed: the caller is already returning an error, and the leftover
// is an orphan the storage page reports rather than a correctness problem.
func (s *Service) purgeObjects(ctx context.Context, keys []string) {
	if s.blob == nil || len(keys) == 0 {
		return
	}
	if err := s.blob.Delete(ctx, keys...); err != nil {
		s.logger.Warn("chat: could not remove orphaned objects", "keys", keys, "err", err)
	}
}

// readMetaField reads one non-file multipart field into the message input.
//
// ⚠ FIELDS ARE READ IN ORDER AND A FIELD AFTER THE FILES STILL COUNTS, unlike
// documents' upload — which returns the moment it sees its single file part. Here
// the loop runs to EOF anyway (there may be ten files), so honouring a trailing
// `body` costs nothing and removes a part-ordering rule from the client contract.
func readMetaField(part *multipart.Part, in *MessageCreate) error {
	buf, err := io.ReadAll(io.LimitReader(part, metaFieldLimit+1))
	if err != nil {
		return httpx.ErrUnprocessable("nelze přečíst pole " + part.FormName())
	}
	if len(buf) > metaFieldLimit {
		return httpx.ErrUnprocessable("pole " + part.FormName() + " je příliš dlouhé")
	}
	switch part.FormName() {
	case "body":
		in.Body = string(buf)
	case "reply_to_id":
		if v := strings.TrimSpace(string(buf)); v != "" {
			in.ReplyToID = &v
		}
	}
	// An unknown field is ignored rather than refused: a client that sends one more
	// form field than this build knows about is not a client to break.
	return nil
}

func readHead(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, sniffLen)
	n, err := f.Read(head)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return head[:n], nil
}
