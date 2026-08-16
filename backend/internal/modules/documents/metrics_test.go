package documents_test

import (
	"context"
	"testing"
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/modules/documents"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/metrics"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/testsupport"
)

func TestDocumentsMetricDescriptor(t *testing.T) {
	x := newH(t)
	got := documents.NewModule(x.svc).MetricProvider().Descriptors()

	if len(got) != 1 || got[0].Key != documents.MetricPinnedCount {
		t.Fatalf("descriptors = %+v, want just %s", got, documents.MetricPinnedCount)
	}
	if got[0].Scope != metrics.ScopePersonal {
		t.Errorf("scope = %q, want personal — personal pins differ per member", got[0].Scope)
	}
}

func TestDocumentsPinnedCountIsPerRecipientAndDeduped(t *testing.T) {
	x := newH(t)
	p := documents.NewModule(x.svc).MetricProvider()
	ctx := context.Background()

	karel := testsupport.CtxUser("karel", "editor")
	eva := testsupport.CtxUser("eva", "editor")

	shared := x.upload(karel, "smlouva.pdf", pdfBytes(), nil)
	karelsOwn := x.upload(karel, "navod.pdf", pdfBytes(), nil)
	evasOwn := x.upload(eva, "recept.pdf", pdfBytes(), nil)
	x.upload(karel, "nepripnuty.pdf", pdfBytes(), nil)

	if _, err := x.svc.Pin(karel, shared.ID, "household", ""); err != nil {
		t.Fatalf("household pin: %v", err)
	}
	if _, err := x.svc.Pin(karel, karelsOwn.ID, "personal", ""); err != nil {
		t.Fatalf("karel personal pin: %v", err)
	}
	if _, err := x.svc.Pin(eva, evasOwn.ID, "personal", ""); err != nil {
		t.Fatalf("eva personal pin: %v", err)
	}
	// The doubly-pinned document must count once (household precedence).
	if _, err := x.svc.Pin(karel, shared.ID, "personal", ""); err != nil {
		t.Fatalf("karel double pin: %v", err)
	}

	karelCount, err := p.Value(ctx, "karel", documents.MetricPinnedCount, time.Now())
	if err != nil {
		t.Fatalf("karel: %v", err)
	}
	evaCount, err := p.Value(ctx, "eva", documents.MetricPinnedCount, time.Now())
	if err != nil {
		t.Fatalf("eva: %v", err)
	}

	if karelCount != 2 {
		t.Errorf("karel = %d, want 2 (household + his own, de-duplicated)", karelCount)
	}
	if evaCount != 2 {
		t.Errorf("eva = %d, want 2 (household + her own)", evaCount)
	}
}

func TestDocumentsMetricUnknownKey(t *testing.T) {
	x := newH(t)
	p := documents.NewModule(x.svc).MetricProvider()
	if _, err := p.Value(context.Background(), "u1", "documents.nonsense", time.Now()); err == nil {
		t.Error("expected an error for an unknown metric key")
	}
}
