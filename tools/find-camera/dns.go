package main

// Just enough of the DNS message format (RFC 1035, as mDNS uses it: RFC 6762)
// to ask one question and read the PTR / SRV / TXT / A / AAAA records of the
// answers back. Other record types are skipped.

import (
	"encoding/binary"
	"errors"
	"net"
	"strings"
)

const (
	typeA    = 1
	typePTR  = 12
	typeTXT  = 16
	typeAAAA = 28
	typeSRV  = 33
	classIN  = 1
)

var errShort = errors.New("truncated DNS message")

type record struct {
	name string // lower case, no trailing dot
	typ  uint16
	// set according to typ
	target string   // PTR, SRV (lower case)
	port   uint16   // SRV
	txt    []string // TXT
	ip     net.IP   // A, AAAA
}

// buildQuery - a query for one name and type (id 0, as mDNS queries have)
func buildQuery(name string, typ uint16) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:], 1) // one question
	b = appendName(b, name)
	b = binary.BigEndian.AppendUint16(b, typ)
	return binary.BigEndian.AppendUint16(b, classIN)
}

func appendName(b []byte, name string) []byte {
	for _, l := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0)
}

// parseResponse - the answer, authority and additional records of a response.
// A query (our own, looped back to us, or another host's) gives none.
func parseResponse(msg []byte) ([]record, error) {
	if len(msg) < 12 {
		return nil, errShort
	}
	if msg[2]&0x80 == 0 {
		return nil, nil
	}
	be := binary.BigEndian
	questions := int(be.Uint16(msg[4:]))
	rrs := int(be.Uint16(msg[6:])) + int(be.Uint16(msg[8:])) + int(be.Uint16(msg[10:]))
	off := 12
	for i := 0; i < questions; i++ {
		_, end, err := readName(msg, off)
		if err != nil {
			return nil, err
		}
		off = end + 4 // type, class
	}
	var out []record
	for i := 0; i < rrs; i++ {
		name, end, err := readName(msg, off)
		if err != nil {
			return nil, err
		}
		if end+10 > len(msg) {
			return nil, errShort
		}
		r := record{name: strings.ToLower(name), typ: be.Uint16(msg[end:])}
		rd, rdlen := end+10, int(be.Uint16(msg[end+8:]))
		if rd+rdlen > len(msg) {
			return nil, errShort
		}
		data := msg[rd : rd+rdlen]
		switch r.typ {
		case typeA, typeAAAA:
			if len(data) == 4 || len(data) == 16 {
				r.ip = append(net.IP(nil), data...)
			}
		case typePTR:
			r.target, _, err = readName(msg, rd)
		case typeSRV:
			if len(data) < 7 {
				return nil, errShort
			}
			r.port = be.Uint16(data[4:])
			r.target, _, err = readName(msg, rd+6)
		case typeTXT:
			for p := 0; p < len(data); {
				l := int(data[p])
				if p+1+l > len(data) {
					return nil, errShort
				}
				r.txt = append(r.txt, string(data[p+1:p+1+l]))
				p += 1 + l
			}
		}
		if err != nil {
			return nil, err
		}
		r.target = strings.ToLower(r.target)
		out = append(out, r)
		off = rd + rdlen
	}
	return out, nil
}

// readName - the name at off, following compression pointers, and the offset
// just after it in the message
func readName(msg []byte, off int) (string, int, error) {
	var labels []string
	end := -1
	for jumps := 0; ; {
		if off >= len(msg) {
			return "", 0, errShort
		}
		l := int(msg[off])
		switch {
		case l == 0:
			if end < 0 {
				end = off + 1
			}
			return strings.Join(labels, "."), end, nil
		case l&0xC0 == 0xC0:
			if off+1 >= len(msg) {
				return "", 0, errShort
			}
			if end < 0 {
				end = off + 2
			}
			if jumps++; jumps > 32 {
				return "", 0, errors.New("DNS name compression loop")
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)
		case l&0xC0 != 0:
			return "", 0, errors.New("bad DNS label")
		default:
			if off+1+l > len(msg) {
				return "", 0, errShort
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
}
