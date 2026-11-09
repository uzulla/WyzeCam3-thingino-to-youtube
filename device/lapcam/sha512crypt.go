package main

// sha512crypt - the $6$ password hash of /etc/shadow (Ulrich Drepper's
// "Unix crypt using SHA-256 and SHA-512", as glibc / musl / uClibc do it),
// written out here so the camera's root password can be checked without a
// crypt(3) binding or a third-party module. Verified against the test vectors
// of the specification (sha512crypt_test.go).

import (
	"crypto/sha512"
	"crypto/subtle"
	"errors"
	"strconv"
	"strings"
)

const (
	cryptB64         = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	sha512DefRounds  = 5000
	sha512MinRounds  = 1000
	sha512MaxRounds  = 999999999
	sha512MaxSaltLen = 16
)

var errNotSHA512Crypt = errors.New("not a $6$ hash")

// sha512Crypt - the full "$6$[rounds=N$]salt$hash" string for password and
// salt (salt is cut to 16 characters as the spec says). roundsGiven controls
// whether "rounds=N$" appears in the result.
func sha512Crypt(password, salt string, rounds int, roundsGiven bool) string {
	if len(salt) > sha512MaxSaltLen {
		salt = salt[:sha512MaxSaltLen]
	}
	if rounds < sha512MinRounds {
		rounds = sha512MinRounds
	}
	if rounds > sha512MaxRounds {
		rounds = sha512MaxRounds
	}
	key := []byte(password)
	sb := []byte(salt)

	// B: password + salt + password
	hb := sha512.New()
	hb.Write(key)
	hb.Write(sb)
	hb.Write(key)
	b := hb.Sum(nil)

	// A: password + salt + (B for each 64-byte block of the password length,
	// then its remainder) + (per bit of the length: B or the password)
	ha := sha512.New()
	ha.Write(key)
	ha.Write(sb)
	for n := len(key); n > 0; n -= 64 {
		if n > 64 {
			ha.Write(b)
		} else {
			ha.Write(b[:n])
		}
	}
	for n := len(key); n > 0; n >>= 1 {
		if n&1 != 0 {
			ha.Write(b)
		} else {
			ha.Write(key)
		}
	}
	a := ha.Sum(nil)

	// P: bytes of DP = sha512(password repeated len(password) times)
	hp := sha512.New()
	for i := 0; i < len(key); i++ {
		hp.Write(key)
	}
	dp := hp.Sum(nil)
	p := make([]byte, 0, len(key))
	for n := len(key); n > 0; n -= 64 {
		if n > 64 {
			p = append(p, dp...)
		} else {
			p = append(p, dp[:n]...)
		}
	}

	// S: bytes of DS = sha512(salt repeated 16 + A[0] times)
	hs := sha512.New()
	for i := 0; i < 16+int(a[0]); i++ {
		hs.Write(sb)
	}
	ds := hs.Sum(nil)
	s := make([]byte, 0, len(sb))
	for n := len(sb); n > 0; n -= 64 {
		if n > 64 {
			s = append(s, ds...)
		} else {
			s = append(s, ds[:n]...)
		}
	}

	// the rounds
	c := a
	for i := 0; i < rounds; i++ {
		h := sha512.New()
		if i&1 != 0 {
			h.Write(p)
		} else {
			h.Write(c)
		}
		if i%3 != 0 {
			h.Write(s)
		}
		if i%7 != 0 {
			h.Write(p)
		}
		if i&1 != 0 {
			h.Write(c)
		} else {
			h.Write(p)
		}
		c = h.Sum(nil)
	}

	var out strings.Builder
	out.WriteString("$6$")
	if roundsGiven {
		out.WriteString("rounds=")
		out.WriteString(strconv.Itoa(rounds))
		out.WriteByte('$')
	}
	out.WriteString(salt)
	out.WriteByte('$')
	enc := func(b2, b1, b0 byte, n int) {
		w := uint32(b2)<<16 | uint32(b1)<<8 | uint32(b0)
		for ; n > 0; n-- {
			out.WriteByte(cryptB64[w&0x3f])
			w >>= 6
		}
	}
	// the byte order of the specification
	enc(c[0], c[21], c[42], 4)
	enc(c[22], c[43], c[1], 4)
	enc(c[44], c[2], c[23], 4)
	enc(c[3], c[24], c[45], 4)
	enc(c[25], c[46], c[4], 4)
	enc(c[47], c[5], c[26], 4)
	enc(c[6], c[27], c[48], 4)
	enc(c[28], c[49], c[7], 4)
	enc(c[50], c[8], c[29], 4)
	enc(c[9], c[30], c[51], 4)
	enc(c[31], c[52], c[10], 4)
	enc(c[53], c[11], c[32], 4)
	enc(c[12], c[33], c[54], 4)
	enc(c[34], c[55], c[13], 4)
	enc(c[56], c[14], c[35], 4)
	enc(c[15], c[36], c[57], 4)
	enc(c[37], c[58], c[16], 4)
	enc(c[59], c[17], c[38], 4)
	enc(c[18], c[39], c[60], 4)
	enc(c[40], c[61], c[19], 4)
	enc(c[62], c[20], c[41], 4)
	enc(0, 0, c[63], 2)
	return out.String()
}

// sha512CryptVerify - does password produce hash (a "$6$..." string from
// /etc/shadow)? Constant-time comparison of the result.
func sha512CryptVerify(password, hash string) (bool, error) {
	rest, ok := strings.CutPrefix(hash, "$6$")
	if !ok {
		return false, errNotSHA512Crypt
	}
	rounds := sha512DefRounds
	roundsGiven := false
	if r, ok := strings.CutPrefix(rest, "rounds="); ok {
		num, after, found := strings.Cut(r, "$")
		if !found {
			return false, errNotSHA512Crypt
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			return false, errNotSHA512Crypt
		}
		rounds = n
		roundsGiven = true
		rest = after
	}
	salt, _, found := strings.Cut(rest, "$")
	if !found {
		return false, errNotSHA512Crypt
	}
	got := sha512Crypt(password, salt, rounds, roundsGiven)
	return subtle.ConstantTimeCompare([]byte(got), []byte(hash)) == 1, nil
}
