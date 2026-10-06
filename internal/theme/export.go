package theme

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Export writes a theme as a theme file with every setting spelled out, as a
// starting point for a theme of one's own. Values the two formats share are
// written once; where DOCX differs, the DOCX value follows under [docx].
// Loading the result gives the same theme back.
func Export(set Set) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# A markout theme, exported from %q.\n", set.PDF.Name)
	b.WriteString(`#
# Save it as NAME.toml and select it with --theme NAME.toml, or put it in
# markout's themes directory to select it by name. Delete what you do not want
# to change: anything left out comes from the theme named by "extends".
#
# Lengths are in points (1/72 inch) unless they carry a unit: "25mm", "2.5cm",
# "1in". Colors are "#RRGGBB". Settings here apply to PDF and DOCX alike; the
# [docx] sections at the end hold what differs in DOCX, and [pdf] works the
# same way.

`)
	fmt.Fprintf(&b, "description = %s\n", strconv.Quote(set.PDF.Description))
	b.WriteString("extends = \"default\"\n")

	pdf, docx := reflect.ValueOf(set.PDF), reflect.ValueOf(set.DOCX)
	writeSections(&b, "", pdf, reflect.Value{})
	writeFamilies(&b, set.PDF.Fonts.Families)

	var diff strings.Builder
	writeSections(&diff, "docx.", docx, pdf)
	if diff.Len() > 0 {
		b.WriteString("\n# ---- DOCX only " + strings.Repeat("-", 60) + "\n")
		b.WriteString(diff.String())
	}
	return b.String()
}

// writeSections writes the sections of a theme. With a valid other, only the
// settings that differ from it are written.
func writeSections(b *strings.Builder, prefix string, th, other reflect.Value) {
	t := th.Type()
	for i := 0; i < t.NumField(); i++ {
		key := t.Field(i).Tag.Get("key")
		if key == "" || key == "-" {
			continue
		}
		field := th.Field(i)
		var otherField reflect.Value
		if other.IsValid() {
			otherField = other.Field(i)
		}
		if field.Kind() == reflect.Array { // the six heading levels
			for n := 0; n < field.Len(); n++ {
				var o reflect.Value
				if otherField.IsValid() {
					o = otherField.Index(n)
				}
				writeTable(b, fmt.Sprintf("%s%s.h%d", prefix, key, n+1), field.Index(n), o)
			}
			continue
		}
		writeTable(b, prefix+key, field, otherField)
	}
}

// writeTable writes one [section] and, after it, its nested sections.
func writeTable(b *strings.Builder, name string, v, other reflect.Value) {
	t := v.Type()
	var lines []string
	type nested struct {
		name     string
		v, other reflect.Value
	}
	var subs []nested

	for i := 0; i < t.NumField(); i++ {
		key := t.Field(i).Tag.Get("key")
		if key == "" || key == "-" {
			continue
		}
		field := v.Field(i)
		var o reflect.Value
		if other.IsValid() {
			o = other.Field(i)
		}
		if field.Kind() == reflect.Struct {
			if _, isColor := field.Interface().(Color); !isColor {
				subs = append(subs, nested{name + "." + key, field, o})
				continue
			}
		}
		if o.IsValid() && reflect.DeepEqual(field.Interface(), o.Interface()) {
			continue
		}
		line := fmt.Sprintf("%s = %s", key, formatValue(field.Interface()))
		if doc := t.Field(i).Tag.Get("doc"); doc != "" && !other.IsValid() {
			line = fmt.Sprintf("%-34s # %s", line, doc)
		}
		lines = append(lines, line)
	}

	if len(lines) > 0 {
		fmt.Fprintf(b, "\n[%s]\n%s\n", name, strings.Join(lines, "\n"))
	}
	for _, sub := range subs {
		writeTable(b, sub.name, sub.v, sub.other)
	}
}

func formatValue(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatFloat(roundTo(x, 3), 'f', -1, 64)
	case Color:
		return strconv.Quote(x.String())
	case string:
		return strconv.Quote(x)
	case bool:
		return strconv.FormatBool(x)
	}
	return fmt.Sprint(v)
}

func roundTo(x float64, places int) float64 {
	scale := 1.0
	for i := 0; i < places; i++ {
		scale *= 10
	}
	if x < 0 {
		return float64(int64(x*scale-0.5)) / scale
	}
	return float64(int64(x*scale+0.5)) / scale
}

func writeFamilies(b *strings.Builder, families map[string]FontFamily) {
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fam := families[name]
		fmt.Fprintf(b, "\n[fonts.family.%s]\n", name)
		for _, kv := range [][2]string{{"regular", fam.Regular}, {"bold", fam.Bold}, {"italic", fam.Italic}, {"bold-italic", fam.BoldItalic}, {"name", fam.Name}} {
			if kv[1] != "" {
				fmt.Fprintf(b, "%s = %s\n", kv[0], strconv.Quote(kv[1]))
			}
		}
	}
}
