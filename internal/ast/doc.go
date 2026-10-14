// Package ast defines the tree a Markdown document is read into and the
// renderers draw from. The parse package builds it, and the render package
// turns it into a DOCX or a PDF file, so neither knows about the other.
//
// A Document is a list of Elements: headings, paragraphs, lists, tables,
// code blocks, quotes, alerts, images and so on. Text inside them is a list
// of runs, each with its own formatting. The package keeps no behaviour of its
// own beyond small helpers, such as Images, which finds every image wherever
// it sits in the tree.
package ast
