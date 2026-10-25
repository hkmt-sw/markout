// Command debug is a development tool. With an output name ending in .docx or
// .pdf it converts a Markdown file, as the markout command does. Without one
// it prints the parsed document as JSON, which shows how the parser read the
// source; MARKOUT_FLAVOR selects the flavor to parse as.
//
// Usage:
//
//	debug input.md [output.docx|output.pdf]
package main
