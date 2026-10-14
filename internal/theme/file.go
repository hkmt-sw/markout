package theme

import (
	"embed"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// A theme file is TOML. It names the theme it builds on with "extends"
// (the default theme if it does not) and lists only what it changes:
//
//	extends = "default"
//
//	[page]
//	size   = "A4"
//	margin = "25mm"
//
//	[heading.h1]
//	size  = 28
//	color = "#1A1A2E"
//
// Settings at the top level apply to both output formats. The same sections
// under [pdf] or [docx] apply to that format only.

// Ext is the file name extension of theme files.
const Ext = ".toml"

// DefaultName is the name of the built-in default theme.
const DefaultName = "default"

// maxDepth bounds a chain of themes extending one another.
const maxDepth = 8

// Info describes a theme that can be selected by name.
type Info struct {
	Name        string
	Description string
	Path        string // file the theme was read from; "" for built-in themes
}

// Loader finds themes by name.
type Loader struct {
	// UserDir is the directory holding the user's own themes, each a
	// NAME.toml file. It may be empty or not exist.
	UserDir string
}

//go:embed builtin/*.toml
var builtinFiles embed.FS

// builtin holds the themes that ship with markout besides the default, as
// theme file text by name.
var builtin = func() map[string]string {
	out := map[string]string{}
	entries, err := builtinFiles.ReadDir("builtin")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		data, err := builtinFiles.ReadFile("builtin/" + e.Name())
		if err != nil {
			panic(err)
		}
		out[strings.TrimSuffix(e.Name(), Ext)] = string(data)
	}
	return out
}()

// List returns the themes that can be selected by name: the built-in ones
// first, then the user's, each group in alphabetical order with the default
// at the top. A user theme that cannot be read is listed with the error as
// its description, so it is visible rather than silently missing.
func (l Loader) List() []Info {
	out := []Info{{Name: DefaultName, Description: Default().PDF.Description}}

	names := make([]string, 0, len(builtin))
	for name := range builtin {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		out = append(out, Info{Name: name, Description: describe(l, name)})
	}

	if l.UserDir != "" {
		files, _ := filepath.Glob(filepath.Join(l.UserDir, "*"+Ext))
		slices.Sort(files)
		for _, file := range files {
			name := strings.TrimSuffix(filepath.Base(file), Ext)
			if name == DefaultName {
				continue
			}
			if _, shadowed := builtin[name]; shadowed {
				continue
			}
			out = append(out, Info{Name: name, Description: describe(l, name), Path: file})
		}
	}
	return out
}

func describe(l Loader, name string) string {
	set, err := l.Load(name)
	if err != nil {
		return "cannot be used: " + err.Error()
	}
	return set.PDF.Description
}

// Load returns the theme ref names: a built-in or user theme by name, or a
// theme file by path (anything ending in .toml or containing a path
// separator).
func (l Loader) Load(ref string) (Set, error) {
	return l.load(ref, "", nil)
}

func (l Loader) load(ref, fromDir string, chain []string) (Set, error) {
	if ref == "" || ref == DefaultName {
		return Default(), nil
	}
	if len(chain) >= maxDepth {
		return Set{}, fmt.Errorf("themes extend one another more than %d levels deep: %s", maxDepth, strings.Join(chain, " -> "))
	}

	name, text, dir, path, err := l.read(ref, fromDir)
	if err != nil {
		return Set{}, err
	}
	id := path
	if id == "" {
		id = "built-in " + name
	}
	for _, seen := range chain {
		if seen == id {
			return Set{}, fmt.Errorf("theme %s extends itself: %s -> %s", name, strings.Join(chain, " -> "), id)
		}
	}

	var raw map[string]any
	if _, err := toml.Decode(text, &raw); err != nil {
		return Set{}, fmt.Errorf("theme %s: %w", label(name, path), err)
	}

	base := DefaultName
	if v, ok := raw["extends"]; ok {
		s, isString := v.(string)
		if !isString || s == "" {
			return Set{}, fmt.Errorf("theme %s: extends must be the name of a theme", label(name, path))
		}
		base = s
	}
	set, err := l.load(base, dir, append(chain, id))
	if err != nil {
		return Set{}, fmt.Errorf("theme %s: %w", label(name, path), err)
	}

	fail := func(err error) (Set, error) {
		return Set{}, fmt.Errorf("theme %s: %w", label(name, path), err)
	}

	common := map[string]any{}
	only := map[string]map[string]any{}
	for key, value := range raw {
		switch key {
		case "extends":
		case "name", "description":
			s, isString := value.(string)
			if !isString {
				return fail(fmt.Errorf("%s must be text in quotes", key))
			}
			if key == "description" {
				set.PDF.Description, set.DOCX.Description = s, s
			}
		case "pdf", "docx":
			m, ok := value.(map[string]any)
			if !ok {
				return fail(fmt.Errorf("[%s] must be a table of sections", key))
			}
			only[key] = m
		default:
			common[key] = value
		}
	}
	if _, ok := raw["description"]; !ok {
		set.PDF.Description, set.DOCX.Description = "", ""
	}

	for _, target := range []struct {
		format string
		theme  *Theme
	}{{"pdf", &set.PDF}, {"docx", &set.DOCX}} {
		if err := apply(target.theme, common, dir, ""); err != nil {
			return fail(err)
		}
		if err := apply(target.theme, only[target.format], dir, target.format+"."); err != nil {
			return fail(err)
		}
		target.theme.Name = name
	}

	resolveDocxFonts(&set.DOCX)
	if err := validate(set.PDF, true); err != nil {
		return fail(err)
	}
	if err := validate(set.DOCX, false); err != nil {
		return fail(fmt.Errorf("for DOCX: %w", err))
	}
	return set, nil
}

func label(name, path string) string {
	if path != "" {
		return path
	}
	return name
}

// read finds the text of a theme: by path, among the built-in themes, or in
// the user's theme directory. fromDir resolves a relative path named by
// another theme file's extends.
func (l Loader) read(ref, fromDir string) (name, text, dir, path string, err error) {
	isPath := strings.HasSuffix(ref, Ext) || strings.ContainsAny(ref, `/\`)
	if !isPath {
		if text, ok := builtin[ref]; ok {
			return ref, text, "", "", nil
		}
		if l.UserDir != "" {
			path = filepath.Join(l.UserDir, ref+Ext)
			if data, rerr := readThemeFile(path); rerr == nil {
				return ref, data, l.UserDir, path, nil
			} else if !os.IsNotExist(rerr) {
				return "", "", "", "", fmt.Errorf("theme %s: %w", ref, rerr)
			}
		}
		return "", "", "", "", fmt.Errorf("there is no theme named %q (see --list-themes)", ref)
	}

	path = ref
	if !filepath.IsAbs(path) && fromDir != "" {
		path = filepath.Join(fromDir, path)
	}
	data, rerr := readThemeFile(path)
	if rerr != nil {
		return "", "", "", "", fmt.Errorf("theme file %s: %w", path, rerr)
	}
	abs, aerr := filepath.Abs(path)
	if aerr != nil {
		abs = path
	}
	return strings.TrimSuffix(filepath.Base(path), Ext), data, filepath.Dir(abs), abs, nil
}

// maxThemeBytes caps the size of a theme file.
const maxThemeBytes = 1 << 20

func readThemeFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxThemeBytes {
		return "", fmt.Errorf("not a theme file")
	}
	data, err := os.ReadFile(path)
	return string(data), err
}

// ---- applying settings ----------------------------------------------------

// apply sets the given sections on a theme. prefix is prepended to setting
// names in error messages ("pdf." for a format-specific section).
func apply(th *Theme, sections map[string]any, dir, prefix string) error {
	names := make([]string, 0, len(sections))
	for name := range sections {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		m, ok := sections[name].(map[string]any)
		if !ok {
			return fmt.Errorf("%s%s must be a section ([%s%s]), not a value", prefix, name, prefix, name)
		}
		path := prefix + name
		var err error
		switch name {
		case "page":
			err = applyPage(&th.Page, m, path)
		case "fonts":
			err = applyFonts(&th.Fonts, m, dir, path)
		case "heading":
			err = applyHeadings(&th.Heading, m, path)
		case "classification":
			err = applyClassification(&th.Classification, m, path)
		default:
			field, found := fieldByKey(reflect.ValueOf(th).Elem(), name)
			if !found {
				return fmt.Errorf("there is no section [%s]; the sections are %s", path, strings.Join(keysOf(reflect.TypeOf(*th)), ", "))
			}
			err = setStruct(field, m, path)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Paper sizes by name, in points.
var paperSizes = map[string][2]float64{
	"a3":      {841.89, 1190.55},
	"a4":      {595.28, 841.89},
	"a5":      {419.53, 595.28},
	"letter":  {612, 792},
	"legal":   {612, 1008},
	"tabloid": {792, 1224},
}

func applyPage(p *Page, m map[string]any, path string) error {
	rest := map[string]any{}
	orientation := ""
	for key, value := range m {
		switch key {
		case "size":
			name, _ := value.(string)
			size, ok := paperSizes[strings.ToLower(name)]
			if !ok {
				return fmt.Errorf("%s.size: %v is not a paper size (A3, A4, A5, Letter, Legal, Tabloid); for another size set width and height", path, value)
			}
			// Keep the orientation the page already has unless it is set too
			if p.Width > p.Height {
				size[0], size[1] = size[1], size[0]
			}
			p.Width, p.Height = size[0], size[1]
		case "orientation":
			orientation, _ = value.(string)
			if orientation != "portrait" && orientation != "landscape" {
				return fmt.Errorf("%s.orientation: %v is neither portrait nor landscape", path, value)
			}
		case "margin":
			all, err := parseLength(value)
			if err != nil {
				return fmt.Errorf("%s.margin: %w", path, err)
			}
			p.MarginTop, p.MarginRight, p.MarginBottom, p.MarginLeft = all, all, all, all
		default:
			rest[key] = value
		}
	}
	// Individual margins and explicit dimensions override the shorthands
	if err := setStruct(reflect.ValueOf(p).Elem(), rest, path); err != nil {
		return err
	}
	if (orientation == "landscape") != (p.Width > p.Height) && orientation != "" {
		p.Width, p.Height = p.Height, p.Width
	}
	return nil
}

func applyFonts(f *Fonts, m map[string]any, dir, path string) error {
	rest := map[string]any{}
	for key, value := range m {
		if key != "family" {
			rest[key] = value
			continue
		}
		families, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.family must hold font definitions, as [%s.family.NAME]", path, path)
		}
		for name, def := range families {
			spec, ok := def.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.family.%s must be a section", path, name)
			}
			if name == "sans" || name == "serif" || name == "mono" {
				return fmt.Errorf("%s.family.%s: %s is a built-in font; choose another name", path, name, name)
			}
			var fam FontFamily
			for key, v := range spec {
				s, isString := v.(string)
				if !isString {
					return fmt.Errorf("%s.family.%s.%s must be text", path, name, key)
				}
				file := s
				if key != "name" && !filepath.IsAbs(file) && dir != "" {
					file = filepath.Join(dir, file)
				}
				switch key {
				case "regular":
					fam.Regular = file
				case "bold":
					fam.Bold = file
				case "italic":
					fam.Italic = file
				case "bold-italic":
					fam.BoldItalic = file
				case "name":
					fam.Name = s
				default:
					return fmt.Errorf("%s.family.%s.%s is not a setting; a font has regular, bold, italic, bold-italic and name", path, name, key)
				}
			}
			if fam.Regular == "" {
				return fmt.Errorf("%s.family.%s needs a regular font file", path, name)
			}
			if f.Families == nil {
				f.Families = map[string]FontFamily{}
			} else {
				// Do not write into a map shared with the theme this one extends
				copied := make(map[string]FontFamily, len(f.Families)+1)
				for k, v := range f.Families {
					copied[k] = v
				}
				f.Families = copied
			}
			f.Families[name] = fam
		}
	}
	return setStruct(reflect.ValueOf(f).Elem(), rest, path)
}

// applyClassification sets the settings of [classification] and its levels,
// the sections [classification.NAME]. A level that exists keeps what the
// section does not set, and the names it lists are added to its own; a new
// level takes the neutral colors until it sets its own.
func applyClassification(c *Classification, m map[string]any, path string) error {
	rest := map[string]any{}
	tables := map[string]map[string]any{}
	for key, value := range m {
		if _, isSetting := fieldByKey(reflect.ValueOf(c).Elem(), key); !isSetting {
			if table, ok := value.(map[string]any); ok {
				tables[key] = table
				continue
			}
		}
		rest[key] = value
	}
	if err := setStruct(reflect.ValueOf(c).Elem(), rest, path); err != nil {
		return err
	}

	// Do not write into a map shared with the theme this one extends
	levels := make(map[string]ClassificationLevel, len(c.Levels)+len(tables))
	for name, level := range c.Levels {
		levels[name] = level
	}
	c.Levels = levels

	for _, name := range sortedNames(tables) {
		level, known := levels[name]
		if !known {
			level = ClassificationLevel{Color: c.Color, Background: c.Background}
		}
		at := path + "." + name
		for _, key := range sortedNames(tables[name]) {
			value := tables[name][key]
			switch key {
			case "names":
				list, ok := value.([]any)
				if !ok {
					return fmt.Errorf("%s.names must be a list of text in quotes, such as [\"internal\", \"belső\"]", at)
				}
				// The names are added to those the level has
				added := append([]string(nil), level.Names...)
				for _, item := range list {
					s, ok := item.(string)
					if !ok || normalizeName(s) == "" {
						return fmt.Errorf("%s.names must be a list of text in quotes, such as [\"internal\", \"belső\"]", at)
					}
					if !slices.ContainsFunc(added, func(have string) bool { return normalizeName(have) == normalizeName(s) }) {
						added = append(added, s)
					}
				}
				level.Names = added
			case "color", "background":
				s, ok := value.(string)
				if !ok {
					return fmt.Errorf("%s.%s: a color is written in quotes, as \"#RRGGBB\"", at, key)
				}
				color, err := ParseColor(s)
				if err != nil {
					return fmt.Errorf("%s.%s: %w", at, key, err)
				}
				if key == "color" {
					level.Color = color
				} else {
					level.Background = color
				}
			default:
				return fmt.Errorf("%s.%s is not a setting; a level has names, color and background", at, key)
			}
		}
		levels[name] = level
	}
	return nil
}

// sortedNames returns the keys of a table in alphabetical order.
func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

var headingKey = regexp.MustCompile(`^h([1-6])$`)

func applyHeadings(h *[6]Heading, m map[string]any, path string) error {
	// A setting directly under [heading] applies to every level; [heading.hN]
	// then adjusts one.
	shared := map[string]any{}
	for key, value := range m {
		if !headingKey.MatchString(key) {
			shared[key] = value
		}
	}
	for i := range h {
		if err := setStruct(reflect.ValueOf(&h[i]).Elem(), shared, path); err != nil {
			return err
		}
	}
	for key, value := range m {
		match := headingKey.FindStringSubmatch(key)
		if match == nil {
			continue
		}
		level, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.%s must be a section", path, key)
		}
		i, _ := strconv.Atoi(match[1])
		if err := setStruct(reflect.ValueOf(&h[i-1]).Elem(), level, path+"."+key); err != nil {
			return err
		}
	}
	return nil
}

// setStruct sets the fields of a struct from a table of settings, matching
// keys to the fields' key tags.
func setStruct(v reflect.Value, m map[string]any, path string) error {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		value := m[key]
		field, found := fieldByKey(v, key)
		if !found {
			return fmt.Errorf("%s.%s is not a setting; [%s] has %s", path, key, path, strings.Join(keysOf(v.Type()), ", "))
		}
		name := path + "." + key
		switch field.Interface().(type) {
		case float64:
			n, err := parseLength(value)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			field.SetFloat(n)
		case Color:
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s: a color is written in quotes, as \"#RRGGBB\"", name)
			}
			c, err := ParseColor(s)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			field.Set(reflect.ValueOf(c))
		case string:
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s must be text in quotes", name)
			}
			field.SetString(s)
		case bool:
			b, ok := value.(bool)
			if !ok {
				return fmt.Errorf("%s must be true or false", name)
			}
			field.SetBool(b)
		default:
			sub, ok := value.(map[string]any)
			if !ok || field.Kind() != reflect.Struct {
				return fmt.Errorf("%s must be a section", name)
			}
			if err := setStruct(field, sub, name); err != nil {
				return err
			}
		}
	}
	return nil
}

func fieldByKey(v reflect.Value, key string) (reflect.Value, bool) {
	t := v.Type()
	for i := range t.NumField() {
		if tag := t.Field(i).Tag.Get("key"); tag == key && tag != "-" {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func keysOf(t reflect.Type) []string {
	var keys []string
	for i := range t.NumField() {
		if tag := t.Field(i).Tag.Get("key"); tag != "" && tag != "-" {
			keys = append(keys, tag)
		}
	}
	slices.Sort(keys)
	return keys
}

var lengthPattern = regexp.MustCompile(`^(-?[0-9]*\.?[0-9]+)\s*(pt|mm|cm|in|px)?$`)

// parseLength reads a length: a number of points, or text with a unit
// ("25mm", "1in", "2.5cm", "12pt", "16px").
func parseLength(value any) (float64, error) {
	switch v := value.(type) {
	case int64:
		return float64(v), nil
	case float64:
		// TOML allows nan and inf, which no comparison in validate catches
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("%v is not a length (use a number of points, or text such as \"25mm\")", v)
		}
		return v, nil
	case string:
		m := lengthPattern.FindStringSubmatch(strings.TrimSpace(strings.ToLower(v)))
		if m == nil {
			return 0, fmt.Errorf("%q is not a length (use a number of points, or a unit: 25mm, 2.5cm, 1in, 12pt)", v)
		}
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil || math.IsInf(n, 0) {
			return 0, fmt.Errorf("%q is not a length", v)
		}
		switch m[2] {
		case "mm":
			n *= 72 / 25.4
		case "cm":
			n *= 72 / 2.54
		case "in":
			n *= 72
		case "px":
			n *= 0.75
		}
		return n, nil
	}
	return 0, fmt.Errorf("%v is not a length (use a number of points, or text such as \"25mm\")", value)
}

// ---- fonts and validation -------------------------------------------------

// BuiltinFonts are the font families every PDF can use, with the font a word
// processor substitutes for each in DOCX.
var BuiltinFonts = map[string]string{
	"sans":  "Arial",
	"serif": "Times New Roman",
	"mono":  "Courier New",
}

// resolveDocxFonts replaces family names in a DOCX theme with the font names
// a word processor knows. Anything that is not a family is taken to be a font
// name already ("Georgia").
func resolveDocxFonts(th *Theme) {
	for _, font := range []*string{&th.Fonts.Body, &th.Fonts.Heading, &th.Fonts.Code} {
		if name, ok := BuiltinFonts[*font]; ok {
			*font = name
		} else if fam, ok := th.Fonts.Families[*font]; ok {
			*font = fam.Name
			if *font == "" {
				*font = strings.TrimSuffix(filepath.Base(fam.Regular), filepath.Ext(fam.Regular))
			}
		}
	}
}

// validateLevels rejects classification levels that no value could be matched
// to, or that two levels claim a name of.
func validateLevels(levels map[string]ClassificationLevel) error {
	owner := map[string]string{} // normalized name -> level
	for _, level := range sortedNames(levels) {
		if len(levels[level].Names) == 0 {
			return fmt.Errorf("classification.%s needs names, the words that mean this level", level)
		}
		for _, name := range levels[level].Names {
			norm := normalizeName(name)
			if other, taken := owner[norm]; taken && other != level {
				return fmt.Errorf("classification: %q is a name of both %s and %s", name, other, level)
			}
			owner[norm] = level
		}
	}
	return nil
}

// The limits of a page, in points. A PDF page is at most 14400 points
// (200 inches) on a side, the largest user space a viewer is required to take.
const (
	minPage    = 72
	maxPage    = 14400
	maxSpacing = 400 // spaces, paddings and indents
)

// validate rejects themes that could not produce a readable document.
func validate(th Theme, pdf bool) error {
	// The comparisons are written so that NaN fails them
	p := th.Page
	if !(p.Width >= minPage && p.Height >= minPage) {
		return fmt.Errorf("page: %g x %g points is too small to be a page", p.Width, p.Height)
	}
	if !(p.Width <= maxPage && p.Height <= maxPage) {
		return fmt.Errorf("page: %g x %g points is too large to be a page; the sides are at most %d points", p.Width, p.Height, maxPage)
	}
	margins := []struct {
		name  string
		value float64
		side  float64
	}{
		{"margin-top", p.MarginTop, p.Height}, {"margin-bottom", p.MarginBottom, p.Height},
		{"margin-left", p.MarginLeft, p.Width}, {"margin-right", p.MarginRight, p.Width},
	}
	for _, m := range margins {
		if !(m.value >= 0) {
			return fmt.Errorf("page.%s is %g points; it cannot be negative", m.name, m.value)
		}
	}
	if p.ContentWidth() < 100 || p.Height-p.MarginTop-p.MarginBottom < 100 {
		return fmt.Errorf("page: the margins leave no room for text on a %.0f x %.0f point page", p.Width, p.Height)
	}
	for _, m := range margins {
		if !(m.value < m.side/2) {
			return fmt.Errorf("page.%s is %g points; it must be less than half of the %g point page", m.name, m.value, m.side)
		}
	}

	if a := th.Text.Align; a != AlignLeft && a != AlignJustify {
		return fmt.Errorf("text.align: %q is neither left nor justify", a)
	}

	if h := th.Text.Hyphenate; h != HyphenateAuto && h != HyphenateAlways && h != HyphenateNever {
		return fmt.Errorf("text.hyphenate: %q is not one of auto, always, never", h)
	}

	sizes := map[string]float64{
		"text.size": th.Text.Size, "text.line-height": th.Text.LineHeight,
		"code.block-size": th.Code.BlockSize, "code.block-line-height": th.Code.BlockLineHeight,
		"table.line-height": th.Table.LineHeight,
		"footnote.size":     th.Footnote.Size, "footnote.line-height": th.Footnote.LineHeight,
		"caption.size":     th.Caption.Size,
		"cover.title-size": th.Cover.TitleSize, "cover.subtitle-size": th.Cover.SubtitleSize,
		"classification.size": th.Classification.Size, "classification.height": th.Classification.Height,
		"watermark.size": th.Watermark.Size,
	}
	if !th.Header.Empty() {
		sizes["header.size"] = th.Header.Size
	}
	if !th.Footer.Empty() {
		sizes["footer.size"] = th.Footer.Size
	}
	// A header or footer sits in the middle of its margin
	if !th.Header.Empty() && p.MarginTop < th.Header.Size+16 {
		return fmt.Errorf("header: the top margin (%.0f points) is too small to hold a header; make page.margin-top at least %.0f", p.MarginTop, th.Header.Size+16)
	}
	if !th.Footer.Empty() && p.MarginBottom < th.Footer.Size+16 {
		return fmt.Errorf("footer: the bottom margin (%.0f points) is too small to hold a footer; make page.margin-bottom at least %.0f", p.MarginBottom, th.Footer.Size+16)
	}
	if err := validateLevels(th.Classification.Levels); err != nil {
		return err
	}
	spacings := map[string]float64{
		"text.paragraph-spacing": th.Text.ParagraphSpacing,
		"code.padding":           th.Code.Padding, "code.space-after": th.Code.SpaceAfter,
		"list.indent": th.List.Indent, "list.space-after": th.List.SpaceAfter,
		"table.cell-padding": th.Table.CellPadding, "table.space-after": th.Table.SpaceAfter,
		"quote.indent": th.Quote.Indent, "quote.bar-width": th.Quote.BarWidth, "quote.space-after": th.Quote.SpaceAfter,
		"rule.width": th.Rule.Width, "rule.space": th.Rule.Space,
		"alert.padding": th.Alert.Padding, "alert.bar-width": th.Alert.BarWidth, "alert.space-after": th.Alert.SpaceAfter,
		"cover.top": th.Cover.Top,
	}
	for i, h := range th.Heading {
		sizes[fmt.Sprintf("heading.h%d.size", i+1)] = h.Size
		sizes[fmt.Sprintf("heading.h%d.line-height", i+1)] = h.LineHeight
		spacings[fmt.Sprintf("heading.h%d.space-before", i+1)] = h.SpaceBefore
		spacings[fmt.Sprintf("heading.h%d.space-after", i+1)] = h.SpaceAfter
	}
	for _, name := range sortedNames(sizes) {
		if v := sizes[name]; !(v >= 1 && v <= 400) {
			return fmt.Errorf("%s is %g points; it must be between 1 and 400", name, v)
		}
	}
	for _, name := range sortedNames(spacings) {
		if v := spacings[name]; !(v >= 0 && v <= maxSpacing) {
			return fmt.Errorf("%s is %g points; it must be between 0 and %d", name, v, maxSpacing)
		}
	}

	if !pdf {
		return nil
	}
	for key, font := range map[string]string{"fonts.body": th.Fonts.Body, "fonts.heading": th.Fonts.Heading, "fonts.code": th.Fonts.Code} {
		if _, ok := BuiltinFonts[font]; ok {
			continue
		}
		fam, ok := th.Fonts.Families[font]
		if !ok {
			return fmt.Errorf("%s: %q is not a font PDF can use; choose sans, serif or mono, or define it under [fonts.family.%s]. To set a font name for DOCX only, put it under [docx.fonts]", key, font, font)
		}
		for _, file := range []string{fam.Regular, fam.Bold, fam.Italic, fam.BoldItalic} {
			if file == "" {
				continue
			}
			info, err := os.Stat(file)
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("fonts.family.%s: font file %s cannot be read", font, file)
			}
		}
	}
	return nil
}
