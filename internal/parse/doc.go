// Package parse reads Markdown into the tree of the package ast.
//
// The core parser is goldmark. Before it runs, a preprocessor rewrites the
// syntax that goldmark has no extension for, such as callouts, content tabs,
// includes and MDX components, into forms it understands; markdown.go builds
// the parser and walks its result into the tree. The satellite files hold the
// parts that need a place of their own: preprocess.go is the preprocessor,
// and extras.go reads alerts, raw HTML, GitLab JSON tables and color chips.
//
// What a document may use depends on its flavor (see the package flavor).
// What the parser leaves out is reported in the Warnings of the document.
package parse
