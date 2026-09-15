// Package extractor pulls stream URLs out of embed pages.
package extractor

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// packedArgs matches the argument list of Dean Edwards' packer:
// eval(function(p,a,c,k,e,d){...}('payload',62,112,'word|list'.split('|'),0,{}))
var packedArgs = regexp.MustCompile(`\}\('((?:[^'\\]|\\.)*)',\s*(\d+),\s*(\d+),\s*'((?:[^'\\]|\\.)*)'\.split\('\|'\)`)

var jsWord = regexp.MustCompile(`\b\w+\b`)

// UnpackAll finds and unpacks every packed script in src.
func UnpackAll(src string) ([]string, error) {
	matches := packedArgs.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		return nil, errors.New("no packed scripts found")
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		radix, _ := strconv.Atoi(m[2])
		count, _ := strconv.Atoi(m[3])
		script, err := unpack(unescapeJS(m[1]), radix, count, strings.Split(unescapeJS(m[4]), "|"))
		if err != nil {
			return nil, err
		}
		out = append(out, script)
	}
	return out, nil
}

// unpack reverses the packer: each word in payload is a number in base radix
// (using the packer's own digit alphabet) indexing into words.
func unpack(payload string, radix, count int, words []string) (string, error) {
	if radix < 2 || radix > 62 {
		return "", fmt.Errorf("unsupported packer radix %d", radix)
	}
	dict := make(map[string]string, count)
	for i := range count {
		key := encodeBase(i, radix)
		if i < len(words) && words[i] != "" {
			dict[key] = words[i]
		} else {
			dict[key] = key
		}
	}
	return jsWord.ReplaceAllStringFunc(payload, func(w string) string {
		if v, ok := dict[w]; ok {
			return v
		}
		return w
	}), nil
}

// encodeBase mirrors the packer's e() function: digits 0-9a-z, then A-Z.
func encodeBase(n, radix int) string {
	var prefix string
	if n >= radix {
		prefix = encodeBase(n/radix, radix)
	}
	d := n % radix
	switch {
	case d > 35:
		return prefix + string(rune(d+29))
	default:
		return prefix + strconv.FormatInt(int64(d), 36)
	}
}

func unescapeJS(s string) string {
	return strings.NewReplacer(`\\`, `\`, `\'`, `'`, `\"`, `"`).Replace(s)
}
