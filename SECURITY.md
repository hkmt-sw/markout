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
- Images referenced by the document are loaded from disk (regular files up to
  20 MB, from any path the document names).
- Images referenced by URL are downloaded over HTTP(S) only after you agree:
  markout lists the servers involved and asks, and skips them when it cannot
  ask. The check looks at the address as written; a public host name that
  resolves to a private address is not recognized as local.
- With the GitLab flavor, `::include{file=…}` reads the named file, but only
  from the directory of the document or below it.
- Nothing is uploaded, and no code from the document is executed.
- Independently of any document, the TUI asks the GitHub API for the latest
  release at most once a day, sending only the markout version, to announce
  updates. `F2` then `u` turns this off; command-line conversions never do it.

Converting a document you did not write therefore lets that document embed
image files from your disk into the output, and, if you allow remote images,
make markout fetch the URLs it names.

## Verifying a download

Each release lists the SHA-256 of its binaries in `SHA256SUMS.txt`, and the
binaries carry a build provenance attestation signed by GitHub Actions. To
check that a binary was built by this repository's release workflow:

```sh
gh attestation verify markout_v1.1.2_darwin_arm64 --repo hkmt-sw/markout
```
