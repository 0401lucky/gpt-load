package parameteroverride

import "testing"

func TestFingerprintDistinguishesEscapedPointerSegments(t *testing.T) {
	t.Parallel()
	var fingerprints []string
	for _, pointer := range []string{"/a~1b", "/a/b"} {
		rules, err := Compile([]any{map[string]any{"remove": []any{pointer}}})
		if err != nil {
			t.Fatal(err)
		}
		fingerprint, err := rules.Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		fingerprints = append(fingerprints, fingerprint)
	}
	if fingerprints[0] == fingerprints[1] {
		t.Fatal("a literal slash and a nested property path share a configuration fingerprint")
	}
}
