package main

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestSigningSubkey(t *testing.T) {
	for _, test := range []struct {
		name string
		key  []byte
	}{
		{name: "binary", key: []byte{0x95, 0x01, 0x00, 0xff}},
		{name: "armored", key: []byte("-----BEGIN PGP PRIVATE KEY BLOCK-----\nsynthetic\n-----END PGP PRIVATE KEY BLOCK-----\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded := base64.StdEncoding.EncodeToString(test.key)
			encoded = "\n" + encoded[:4] + "\r\n" + encoded[4:] + "\n"
			t.Setenv("APT_SIGNING_SUBKEY", encoded)
			secret, err := signingSubkey()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(secret, test.key) {
				t.Fatal("decoded subkey differs from export")
			}
		})
	}
}

func TestSigningSubkeyRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct {
		name, value, message string
	}{
		{name: "missing", message: "APT_SIGNING_SUBKEY is missing"},
		{name: "blank", value: " \r\n", message: "APT_SIGNING_SUBKEY is missing"},
		{name: "invalid", value: "synthetic-secret!", message: "APT_SIGNING_SUBKEY must be a base64-encoded secret subkey export"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("APT_SIGNING_SUBKEY", test.value)
			secret, err := signingSubkey()
			if err == nil || err.Error() != test.message {
				t.Fatalf("expected %q", test.message)
			}
			if secret != nil {
				t.Fatal("invalid input returned secret material")
			}
			if test.value != "" && strings.Contains(err.Error(), test.value) {
				t.Fatal("error discloses secret input")
			}
		})
	}
}
