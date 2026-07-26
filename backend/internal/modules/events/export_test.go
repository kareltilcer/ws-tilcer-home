package events

import (
	"time"

	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/dates"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/registry"
)

// Test seams: build the widget providers with an injectable "today" for the
// external events_test package (an in-package test would cycle through
// testsupport → bootstrap → events).

func NewPripominkyProviderForTest(store *Store, lookbackDays, maxOcc int, today func() dates.Date) registry.WidgetProvider {
	p := newPripominkyProvider(store, time.UTC, lookbackDays, maxOcc)
	p.today = today
	return p
}

func NewTentoMesicProviderForTest(store *Store, maxOcc int, today func() dates.Date) registry.WidgetProvider {
	p := newTentoMesicProvider(store, time.UTC, maxOcc)
	p.today = today
	return p
}
