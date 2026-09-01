package documents

// The permanent address of a document's content is ID-BASED (D42): neither the id
// nor the bytes ever change, so these five URLs are stable for the document's life
// — a rename or a move does not affect them. The slug path is navigation only and
// is deliberately NOT permanent.
//
// One footnote: the preview URL also carries a `?sandbox` cache key, whose value
// changes when the PDF sandbox policy does. The ADDRESS is still permanent — the
// path is what a link resolves — and the query exists only so that a year-old cache
// entry cannot go on answering with a year-old security header.
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
		// The path is the permanent part; the query is previewSandboxVersion, the
		// cache key for the PDF sandbox policy this endpoint serves (content.go).
		// The server stamps it because the server owns that policy — a key kept on
		// the client is a key that can be forgotten in the edit that changes the
		// header it busts.
		Preview:   apiBase + id + "/preview?sandbox=" + previewSandboxVersion,
		Thumbnail: thumbnailURL(id),
	}
}

func thumbnailURL(id string) string { return apiBase + id + "/thumbnail" }
