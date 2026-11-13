package idgen

import "testing"

func TestLengthIsBounded(t *testing.T) {
	for name, call := range map[string]func(){
		"negative":     func() { Password(-1) },
		"too long":     func() { Token(maxLength + 1) },
		"base64":       func() { Base64(maxLength + 1) },
		"with symbols": func() { PasswordWithSymbols(maxLength + 1) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not fail fast", name)
				}
			}()
			call()
		}()
	}
	if got := len(Password(128)); got != 128 {
		t.Fatalf("Password(128) has %d characters", got)
	}
}
