package pdd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// encoding/json silently accepts repeated struct keys, even when their spelling
// differs only in case. A reviewed intent artifact must have one interpretation.
func uniqueJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value func(string) error
	value = func(path string) error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("invalid JSON object key")
				}
				normalized := strings.Map(func(r rune) rune {
					smallest := r
					for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
						if next < smallest {
							smallest = next
						}
					}
					return smallest
				}, key)
				if seen[normalized] {
					return fmt.Errorf("duplicate JSON key %s.%s", path, key)
				}
				seen[normalized] = true
				if err = value(path + "." + key); err != nil {
					return err
				}
			}
		case '[':
			for index := 0; decoder.More(); index++ {
				if err = value(fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
		_, err = decoder.Token()
		return err
	}
	return value("$")
}
