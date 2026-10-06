package render

import _ "embed"

// Embedded fonts so PDF output works on every OS (and renders Hungarian
// accents — including ő/ű — correctly) without depending on system fonts.
//
// Liberation Sans/Mono are metric-compatible with Arial/Courier New and cover
// Latin Extended-A in full. Licensed under the SIL Open Font License 1.1;
// see fonts/LICENSE.
var (
	//go:embed fonts/LiberationSans-Regular.ttf
	fontSansRegular []byte
	//go:embed fonts/LiberationSans-Bold.ttf
	fontSansBold []byte
	//go:embed fonts/LiberationSans-Italic.ttf
	fontSansItalic []byte
	//go:embed fonts/LiberationSans-BoldItalic.ttf
	fontSansBoldItalic []byte
	//go:embed fonts/LiberationMono-Regular.ttf
	fontMonoRegular []byte
)
