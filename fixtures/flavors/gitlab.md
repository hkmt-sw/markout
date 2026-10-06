;;;
{"title": "GitLab Flavored Markdown", "author": "Markout"}
;;;

# GitLab (GLFM)

[[_TOC_]]

## Inline diff

This was {- removed -} and this was {+ added +}, also [- old -] and [+ new +].

## Multiline blockquote

>>>
First quoted paragraph.

Second quoted paragraph.
>>>

## Alerts with titles

> [!warning] Data loss
> Back up first.

## Tasks

- [x] Done
- [~] Not applicable
- [ ] Open

## Math and colors

Inline $`a^2 + b^2 = c^2`$ and `hsl(210, 80%, 45%)` and `#FC6D26`.

## Sized image

![Logo](logo.png){width=50%}

## JSON table

```json:table
{
  "fields": [{"key": "name", "label": "Name"}, "role"],
  "items": [{"name": "Ada", "role": "Engineer"}, {"name": "Linus", "role": "Maintainer"}]
}
```

## Include

::include{file=include-part.md}

Term
: Definition of the term

Emoji :tada:
