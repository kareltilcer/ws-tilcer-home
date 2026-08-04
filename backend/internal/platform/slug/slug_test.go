package slug

import "testing"

func TestMake(t *testing.T) {
	cases := map[string]string{
		"Guláš":                  "gulas",
		"Recepty":                "recepty",
		"Zaplatit plyn":          "zaplatit-plyn",
		"  Servis   kotle  ":     "servis-kotle",
		"Wi-Fi heslo":            "wi-fi-heslo",
		"Č. účtu 123/456":        "c-uctu-123456",
		"Příšerně žluťoučký":     "priserne-zlutoucky",
		"Výměna baterie v kotli": "vymena-baterie-v-kotli",
		"!!!":                    "", // only punctuation → empty (caller falls back to id)
		"---":                    "",
	}
	for in, want := range cases {
		if got := Make(in); got != want {
			t.Errorf("Make(%q) = %q, want %q", in, got, want)
		}
	}
}
