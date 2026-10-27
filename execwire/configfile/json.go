// Package configfile implements bounded, strict explicit configuration input.
package configfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
)

const MaxBytes = 1 << 20

var ErrInput = errors.New("config: invalid, duplicate, unknown or unsafe explicit input")

// Read accepts only a regular file and rejects symlinks in every path component.
// The path is explicit; no referenced storage or credentials are opened.
func Read(path string) ([]byte, error) {
	path, err := filepath.Abs(path)
	// macOS exposes standard temporary roots through system aliases. Normalize
	// those two fixed aliases only; user-controlled links remain rejected.
	if runtime.GOOS == "darwin" {
		for _, prefix := range []string{"/var/", "/tmp/"} {
			if strings.HasPrefix(path, prefix) {
				path = "/private" + path
				break
			}
		}
	}

	if err != nil {
		return nil, ErrInput
	}
	check := func() (os.FileInfo, error) {
		var info os.FileInfo
		p := string(filepath.Separator)
		for _, part := range strings.Split(strings.TrimPrefix(path, p), string(filepath.Separator)) {
			p = filepath.Join(p, part)
			info, err = os.Lstat(p)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil, errors.Join(ErrInput, syscall.ENOENT)
				}
				return nil, ErrInput
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, ErrInput
			}
		}
		if !info.Mode().IsRegular() || info.Size() > MaxBytes {
			return nil, ErrInput
		}
		return info, nil
	}
	before, err := check()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrInput
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, ErrInput
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil || len(b) > MaxBytes {
		return nil, ErrInput
	}
	after, err := check()
	if err != nil || !os.SameFile(opened, after) {
		return nil, ErrInput
	}
	return b, nil
}

// JSON rejects duplicate (including differently cased), unknown, scalar null, trailing
// and deeply nested data before decoding into the actual production type.
func JSON(b []byte, target any) error {
	if len(b) == 0 || len(b) > MaxBytes {
		return ErrInput
	}
	d := json.NewDecoder(bytes.NewReader(b))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return ErrInput
		}
		tok, err := d.Token()
		if err != nil {
			return ErrInput
		}
		if tok == nil {
			if depth == 0 {
				return ErrInput
			}
			return nil
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				key, ok := k.(string)
				key = strings.ToLower(key)
				if e != nil || !ok || seen[key] {
					return ErrInput
				}
				seen[key] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e := walk(depth + 1); e != nil {
					return e
				}
			}
		default:
			return ErrInput
		}
		_, err = d.Token()
		return err
	}
	if err := walk(0); err != nil {
		return ErrInput
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInput
	}
	var raw any
	rawDecoder := json.NewDecoder(bytes.NewReader(b))
	rawDecoder.UseNumber()
	if rawDecoder.Decode(&raw) != nil || !validNulls(raw, reflect.TypeOf(target)) {
		return ErrInput
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(target); err != nil {
		return ErrInput
	}
	return nil
}

func validNulls(v any, t reflect.Type) bool {
	if t == nil {
		return false
	}
	if v == nil {
		switch t.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			return true
		}
		return false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch value := v.(type) {
	case map[string]any:
		if t.Kind() == reflect.Map {
			for _, child := range value {
				if !validNulls(child, t.Elem()) {
					return false
				}
			}
			return true
		}
		if t.Kind() != reflect.Struct {
			return true
		}
		for key, child := range value {
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if f.PkgPath != "" {
					continue
				}
				name := strings.Split(f.Tag.Get("json"), ",")[0]
				if name == "" {
					name = f.Name
				}
				if strings.EqualFold(name, key) && !validNulls(child, f.Type) {
					return false
				}
			}
		}
	case []any:
		if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			for _, child := range value {
				if !validNulls(child, t.Elem()) {
					return false
				}
			}
		}
	}
	return true
}
