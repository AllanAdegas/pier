package secretbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// errNotJSON is returned when a registered JSON field holds something else.
// It never carries the stored value.
var errNotJSON = errors.New("secretbox: registered JSON field does not hold a single JSON document")

// resealJSONPaths rewrites the legacy ciphertexts found at paths inside a
// JSON document. Only string values sitting exactly at a declared path and
// starting with "v1:" are touched: other keys, and any "v1:" text elsewhere
// in the document, are left alone. It returns how many values it rewrote;
// when that is zero, document comes back byte for byte. A rewritten document
// is re-encoded, so object keys come out sorted (no reader depends on order).
func (b *Box) resealJSONPaths(document string, paths []string) (string, int, error) {
	root, err := decodeDocument(document)
	if err != nil {
		return "", 0, err
	}
	count := 0
	for _, path := range paths {
		object, key, found := parentObject(root, strings.Split(path, "."))
		if !found {
			continue
		}
		text, isString := object[key].(string)
		if !isString || !IsLegacy(text) {
			continue
		}
		if object[key], err = b.reseal(text); err != nil {
			return "", 0, err
		}
		count++
	}
	if count == 0 {
		return document, 0, nil
	}
	encoded, err := encodeDocument(root)
	return encoded, count, err
}

func decodeDocument(document string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, errNotJSON
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errNotJSON
	}
	return root, nil
}

// parentObject walks keys down nested objects and returns the object that
// holds the last key.
func parentObject(root any, keys []string) (map[string]any, string, bool) {
	current := root
	for _, key := range keys[:len(keys)-1] {
		object, isObject := current.(map[string]any)
		if !isObject {
			return nil, "", false
		}
		current = object[key]
	}
	object, isObject := current.(map[string]any)
	if !isObject {
		return nil, "", false
	}
	_, present := object[keys[len(keys)-1]]
	return object, keys[len(keys)-1], present
}

func encodeDocument(root any) (string, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(root); err != nil {
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}
