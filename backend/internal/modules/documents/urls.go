package documents

// The permanent address of a document's content is ID-BASED (D42): neither the id
// nor the bytes ever change, so these five URLs are stable for the document's life
// — a rename or a move does not affect them. The slug path is navigation only and
// is deliberately NOT permanent.
//
// Every URL here is served BY THE BACKEND and gated by home's session (D33).
// Object-storage keys never leave the server and no presigned URL is ever handed
// to the browser.

const (
	apiBase       = "/api/documents/"
	permalinkBase = "/d/"
)

// urlsFor builds the id-based URL block for a document. Paths are relative, which
// is correct for home's same-origin deploy; Service.withPublicBase absolutises the
// permalink when HOME_DOCS_PUBLIC_BASE_URL is configured (for pasting a link into
// a chat rather than into the address bar).
func urlsFor(id string) DocumentUrls {
	return DocumentUrls{
		Permalink: permalinkBase + id,
		Raw:       apiBase + id + "/raw",
		Download:  apiBase + id + "/download",
		Preview:   apiBase + id + "/preview",
		Thumbnail: thumbnailURL(id),
	}
}

func thumbnailURL(id string) string { return apiBase + id + "/thumbnail" }
