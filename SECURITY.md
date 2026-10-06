# Security policy

## Supported versions

Fixes are made on the latest release only.

## Reporting a vulnerability

Please do not open a public issue for a security problem. Report it privately
through GitHub: on the repository's **Security** tab choose **Report a
vulnerability**.

Include the markout version (`markout --version`), your operating system, and
a Markdown file or steps that reproduce the problem. You should get a reply
within a week.

## What markout does with your files

Knowing this helps judge whether something is a vulnerability:

- markout reads the Markdown file you point it at and writes one output file.
- Images referenced by the document are loaded from disk, relative to the
  document, or downloaded over HTTP(S) if the reference is a URL.
- With the GitLab flavor, `::include{file=…}` reads the named file from disk.
- Nothing is uploaded, and no code from the document is executed.

Converting a document you did not write therefore lets that document make
markout read local files and fetch URLs of its choosing.
