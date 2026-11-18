package controllers

import "testing"

func TestShortCommit(t *testing.T) {
	for id, want := range map[string]string{
		"":          "",
		"DEV888888": "DEV88888",
		"17e4e9696f35f40f57890d540301fc6f9ff76f76": "17e4e969",
	} {
		if got := shortCommit(id); got != want {
			t.Errorf("shortCommit(%q) = %q, want %q", id, got, want)
		}
	}
}
