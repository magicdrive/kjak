package configure

import (
	"bytes"
	"reflect"
)

// appendOnly writes a change that only adds feathers, groups or templates as
// the current file followed by the new tables, so that the file keeps what
// it had (its comments, its layout). It is used only when the result, read
// back, is exactly what rewriting the file would give (the same Encode): any
// other change, or a file the appended tables would not fit (a feathers
// table written inline, ...), is rewritten as before.
func appendOnly(current []byte, after *File) ([]byte, bool) {
	before, err := Parse(current)
	if err != nil || !reflect.DeepEqual(before.Defaults, after.Defaults) {
		return nil, false
	}
	added := &File{}
	if !addedEntries(before.Feathers, after.Feathers, &added.Feathers) ||
		!addedEntries(before.Groups, after.Groups, &added.Groups) ||
		!addedEntries(before.Templates, after.Templates, &added.Templates) {
		return nil, false
	}
	if len(added.Feathers)+len(added.Groups)+len(added.Templates) == 0 {
		return current, true // nothing changed
	}
	tables, err := Encode(added)
	if err != nil {
		return nil, false
	}
	// [feathers.NAME] and the like only: the file may define the parent
	// tables already
	var b bytes.Buffer
	b.Write(current)
	if len(current) > 0 && current[len(current)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	for _, line := range bytes.SplitAfter(tables, []byte("\n")) {
		switch string(bytes.TrimSpace(line)) {
		case "[feathers]", "[groups]", "[templates]":
			continue
		}
		b.Write(line)
	}
	kept := b.Bytes()
	back, err := Parse(kept)
	if err != nil {
		return nil, false
	}
	want, err1 := Encode(after)
	got, err2 := Encode(back)
	if err1 != nil || err2 != nil || !bytes.Equal(want, got) {
		return nil, false
	}
	return kept, true
}

// addedEntries collects into out the entries of after that before does not
// have; false when an entry of before was changed or removed.
func addedEntries[V any](before, after map[string]V, out *map[string]V) bool {
	for name, v := range before {
		w, ok := after[name]
		if !ok || !reflect.DeepEqual(v, w) {
			return false
		}
	}
	for name, v := range after {
		if _, ok := before[name]; !ok {
			if *out == nil {
				*out = map[string]V{}
			}
			(*out)[name] = v
		}
	}
	return true
}

// hasComments reports whether a TOML file seems to have a comment: a line
// that starts with "#", or a " #" after a value (a "#" inside a string may
// be counted too: the warning is then one too many, never one too few).
func hasComments(content []byte) bool {
	for _, line := range bytes.Split(content, []byte("\n")) {
		t := bytes.TrimSpace(line)
		if bytes.HasPrefix(t, []byte("#")) || bytes.Contains(t, []byte(" #")) || bytes.Contains(t, []byte("\t#")) {
			return true
		}
	}
	return false
}
