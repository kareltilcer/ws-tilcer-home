// Command vapidgen prints a fresh VAPID keypair for the Web Push channel
// (PRD §V5-9, HANDOFF-7 §14).
//
// Run it ONCE and paste the output into the Coolify secrets:
//
//	go run ./cmd/vapidgen
//
// The keypair identifies this server to every browser push service, so it is a
// SECRET (never committed) and it is NOT rotated casually: a new keypair silently
// invalidates every existing subscription, and each household device would have
// to open Nastavení → Oznámení and subscribe again.
package main

import (
	"fmt"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func main() {
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vapidgen:", err)
		os.Exit(1)
	}
	fmt.Printf("HOME_VAPID_PUBLIC_KEY=%s\n", pub)
	fmt.Printf("HOME_VAPID_PRIVATE_KEY=%s\n", priv)
	fmt.Println(`HOME_VAPID_SUBJECT=mailto:karel@tilcer.cz   # or https://home.tilcer.cz`)
	fmt.Fprintln(os.Stderr, "\nStore these in Coolify. Generate once — rotating invalidates every existing subscription.")
}
