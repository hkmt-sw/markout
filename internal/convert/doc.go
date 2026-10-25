// Package convert turns a Markdown file into a DOCX or a PDF file. It is the
// entry point the terminal UI and the command line share: it reads the source,
// parses it with the package parse in the chosen flavor, adds the version
// history, and hands the tree to the package render with the theme and the
// other options.
//
// It also finds out which servers a document's images would be downloaded
// from, so that the caller can ask the user before anything is fetched.
package convert
