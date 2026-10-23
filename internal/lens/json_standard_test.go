package lens

import (
	"encoding/json"
	"math/rand"
	"testing"
)

// The reader keeps where each part lies, which encoding/json does not hand back, but what it takes
// for JSON is what RFC 8259 says and encoding/json accepts: a body one calls broken is broken.
func TestTheReaderAcceptsExactlyWhatEncodingJSONAccepts(t *testing.T) {
	bom := string(rune(0xfeff))
	lineSeparator := string(rune(0x2028))
	for _, text := range []string{
		`01`, `-`, `-0`, `-01`, `1.`, `.5`, `+1`, `1e`, `1e+`, `1E5`, `0.0e-0`, `1.5E+3`,
		`NaN`, `Infinity`, `-Infinity`, `tru`, `true `, ` true`, `nul`, `[true false]`,
		"\"a\tb\"", "\"a\nb\"", `"\x41"`, `"\u00zz"`, `"\ud800"`, `"😀"`, `"\/"`, `"\a"`,
		`[1,]`, `[,1]`, `{"a":1,}`, `{"a" 1}`, `{a:1}`, `{'a':1}`, `[1 2]`, `{"a":1 "b":2}`,
		`{"a":1}{"b":2}`, `[]]`, `[`, `{`, `[01]`, `[1.]`, `[1e]`, `{"a":-00.1}`,
		bom + "{}", "{}\x00", "[1e999]", "[-0.0]", `{"a":{"a":{}}}`, "[\"" + lineSeparator + "\"]",
		`{"a":1,"a":2}`, ``, ` `, `null`, `"`, `"abc`, `[1,2,3`, `{"a":}`, `{"a":1,,"b":2}`,
	} {
		_, err := parseJSON(text)
		if got, want := err == nil, json.Valid([]byte(text)); got != want {
			t.Errorf("parseJSON(%q) accepted = %v, encoding/json accepted = %v", text, got, want)
		}
	}
}

func TestTheReaderAgreesWithEncodingJSONOnDamagedBodies(t *testing.T) {
	seeds := []string{
		`{"a":[1,2.5,-3e2,true,null,"xé\n"],"b":{"c":"d"}}`,
		`[0,-0,0.1,1e1,"😀"]`,
	}
	alphabet := []byte("{}[]\",:.-+eE0123456789 tfnul\\ux")
	random := rand.New(rand.NewSource(1))

	for range 20000 {
		damaged := []byte(seeds[random.Intn(len(seeds))])
		for edits := random.Intn(3) + 1; edits > 0 && len(damaged) > 1; edits-- {
			at := random.Intn(len(damaged))
			switch random.Intn(3) {
			case 0:
				damaged[at] = alphabet[random.Intn(len(alphabet))]
			case 1:
				damaged = append(damaged[:at], damaged[at+1:]...)
			default:
				inserted := append([]byte{alphabet[random.Intn(len(alphabet))]}, damaged[at:]...)
				damaged = append(damaged[:at], inserted...)
			}
		}
		_, err := parseJSON(string(damaged))
		if got, want := err == nil, json.Valid(damaged); got != want {
			t.Fatalf("parseJSON(%q) accepted = %v, encoding/json accepted = %v", damaged, got, want)
		}
	}
}
