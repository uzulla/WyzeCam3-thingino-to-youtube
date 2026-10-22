package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// what encoding/json + flatten make of the same input, numbers kept as text
func stdFlat(t *testing.T, s string) Values {
	t.Helper()
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("%s: %v", s, err)
	}
	v := Values{}
	if err := flatten("", obj, v); err != nil {
		t.Fatal(err)
	}
	for k, x := range v { // flatJSON skips arrays
		if _, ok := x.([]any); ok {
			delete(v, k)
		}
	}
	return v
}

func TestFlatJSONMatchesEncodingJSON(t *testing.T) {
	for _, s := range []string{
		`{"type":"racebox","seq":7,"device":"RaceBox Mini 0123456789","itow":118286240,
		 "time":"2022-01-10T08:51:08.239971626Z","time_valid":true,"time_confirmed":false,"fix":3,
		 "lat":42.6719035,"lon":23.2887238,"alt_wgs_m":625.761,"heading_deg":0.00000,"pdop":3.00,
		 "battery":{"percent":89,"charging":false},"g":{"x":-0.003,"y":0.113,"z":0.974},"gyro_dps":{"x":-2.09,"y":0.86,"z":-0.04}}`,
		`{"time":null,"a":{"b":{"c":1e-3}},"neg":-0,"big":1E+400}`,
		` { "esc" : "a\"b\\c\/d\u00e9\n" , "list":[1,[2,"]"],{"x":3}], "after":"ok" } `,
		`{}`,
		`{"o":{}}`,
	} {
		got := Values{}
		if err := flatJSON([]byte(s), got); err != nil {
			t.Errorf("%s: %v", s, err)
			continue
		}
		if want := stdFlat(t, s); !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got  %v\n want %v", s, got, want)
		}
	}
}

func TestFlatJSONRejects(t *testing.T) {
	for _, s := range []string{
		``, `not json`, `[1,2]`, `"str"`, `{`, `{"a"}`, `{"a":}`, `{"a":1,}`, `{"a":1 "b":2}`,
		`{"a":01}`, `{"a":1.}`, `{"a":.5}`, `{"a":1e}`, `{"a":--1}`, `{"a":tru}`, `{"a":"x}`,
		`{"a":[1,2}`, `{"a":[1,]}`, `{"a":[1 2]}`, `{"a":[[1],,]}`, `{"a":1} {"b":2}`, `{"a":1}x`, `{"g":{"x":1},"g_x":2}`,
	} {
		if err := flatJSON([]byte(s), Values{}); err == nil {
			t.Errorf("%q must fail", s)
		}
	}
}

func BenchmarkFlatJSONRacebox(b *testing.B) {
	msg := []byte(`{"type":"racebox","seq":223566,"device":"RaceBox Mini 1254301304","itow":383011560,"time":"2026-10-22T10:23:13.560473745Z","time_valid":true,"time_confirmed":true,"time_acc_ns":36,"fix":3,"fix_ok":false,"sats":9,"lat":35.6598543,"lon":139.3300700,"latlon_valid":true,"alt_wgs_m":165.312,"alt_msl_m":126.494,"h_acc_m":4.010,"v_acc_m":5.454,"speed_mps":0.270,"speed_acc_mps":0.429,"heading_deg":37.40988,"heading_acc_deg":43.82910,"heading_valid":false,"pdop":2.57,"battery":{"percent":100,"charging":false},"g":{"x":-0.027,"y":-0.018,"z":0.998},"gyro_dps":{"x":-2.19,"y":1.49,"z":1.07}}`)
	for i := 0; i < b.N; i++ {
		flatJSON(msg, Values{})
	}
}
