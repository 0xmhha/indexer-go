package api

import "testing"

func TestRedactURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://mainnet.infura.io/v3/0123456789abcdef":       "https://mainnet.infura.io",
		"https://user:secret@node.example.com:8545/rpc?key=1": "https://node.example.com:8545",
		"ws://127.0.0.1:8546":                                 "ws://127.0.0.1:8546",
		"":                                                    "",
		"not a url":                                           "",
	} {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, want)
		}
	}
}
