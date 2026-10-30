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
	// the line ends of the file (CRLF from a Windows editor) for what is added
	eol := []byte("\n")
	if bytes.Contains(current, []byte("\r\n")) {
		eol = []byte("\r\n")
	}
	var b bytes.Buffer
	b.Write(current)
	if len(current) > 0 && current[len(current)-1] != '\n' {
		b.Write(eol)
	}
	b.Write(eol)
	for _, line := range bytes.SplitAfter(tables, []byte("\n")) {
		switch string(bytes.TrimSpace(line)) {
		case "[feathers]", "[groups]", "[templates]":
			continue
		}
		if bytes.HasSuffix(line, []byte("\n")) {
			line = append(bytes.TrimSuffix(line, []byte("\n")), eol...)
		}
		b.Write(line)
	}
	kept := b.Bytes()
	back, err := Parse(kept)
	if err != nil {
		return nil, false
	}
	// a guard: no file was found whose appended tables parse but read
	// differently (an inline feathers table fails to parse instead)
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

// hasComments reports whether a TOML file may have a comment: any "#" (a
// comment, also right after a value as in `"os"#note`, or a "#" inside a
// string: the note is then one too many, never one too few).
func hasComments(content []byte) bool {
	return bytes.IndexByte(content, '#') >= 0
}
