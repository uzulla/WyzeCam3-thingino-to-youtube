package main

import "testing"

// the test vectors of the specification (Drepper, "Unix crypt using SHA-256
// and SHA-512", section "Test vectors for SHA-512")
func TestSHA512CryptVectors(t *testing.T) {
	cases := []struct{ hash, password string }{
		{"$6$saltstring$svn8UoSVapNtMuq1ukKS4tPQd8iKwSMHWjl/O817G3uBnIFNjnQJuesI68u4OTLiBFdcbYEdFCoEOfaS35inz1", "Hello world!"},
		{"$6$rounds=10000$saltstringsaltst$OW1/O6BYHV6BcXZu8QVeXbDWra3Oeqh0sbHbbMCVNSnCM/UrjmM0Dp8vOuZeHBy/YTBmSK6H9qs/y3RnOaw5v.", "Hello world!"},
		{"$6$rounds=5000$toolongsaltstrin$lQ8jolhgVRVhY4b5pZKaysCLi0QBxGoNeKQzQ3glMhwllF7oGDZxUhx1yxdYcz/e1JSbq3y6JMxxl8audkUEm0", "This is just a test"},
		{"$6$rounds=1400$anotherlongsalts$POfYwTEok97VWcjxIiSOjiykti.o/pQs.wPvMxQ6Fm7I6IoYN3CmLs66x9t0oSwbtEW7o7UmJEiDwGqd8p4ur1", "a very much longer text to encrypt.  This one even stretches over morethan one line."},
	}
	for _, c := range cases {
		ok, err := sha512CryptVerify(c.password, c.hash)
		if err != nil || !ok {
			t.Errorf("verify(%q, %q) = %v, %v; want true", c.password, c.hash, ok, err)
		}
		ok, _ = sha512CryptVerify(c.password+"x", c.hash)
		if ok {
			t.Errorf("verify with a wrong password accepted for %q", c.hash)
		}
	}
	if _, err := sha512CryptVerify("x", "$1$abc$def"); err == nil {
		t.Errorf("a $1$ hash was not rejected")
	}
}
