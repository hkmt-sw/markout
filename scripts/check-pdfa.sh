#!/bin/sh
# Converts the sample documents to PDF/A with markout and validates each file
# with veraPDF against PDF/A-2u. It fails if any file does not conform.
#
#   scripts/check-pdfa.sh MARKOUT VERAPDF [OUTDIR]
#
# MARKOUT is the markout binary, VERAPDF the verapdf command, and OUTDIR where
# the PDFs are put (default: a new temporary directory).
set -eu

markout=$1
verapdf=$2
out=${3:-$(mktemp -d)}
mkdir -p "$out"

# Saved settings of the machine must not change the output
MARKOUT_CONFIG=$out/config.json
export MARKOUT_CONFIG

flavors=$("$markout" --list-flavors | awk '$1 != "" && $1 != "*" { print $1 } $1 == "*" { print $2 }')
is_flavor() { echo "$flavors" | grep -qx "$1"; }

count=0
convert() { # file flavor
	name=$(echo "$1" | tr '/' '-')
	"$markout" --pdfa --flavor "$2" "$1" "$out/${name%.*}.pdf" >/dev/null
	count=$((count + 1))
}

# The samples are read as the golden tests read them: a document in a
# directory named after a flavor is in that flavor, the others in the default
for f in fixtures/*.md; do convert "$f" markout; done
for f in fixtures/flavors/*.md; do
	flavor=$(basename "$f" .md)
	if is_flavor "$flavor"; then convert "$f" "$flavor"; fi
done
for f in examples/*/*.md*; do
	flavor=$(basename "$(dirname "$f")")
	is_flavor "$flavor" || flavor=markout
	convert "$f" "$flavor"
done

echo "Validating $count PDF/A files with veraPDF"
report=$out/report.txt
"$verapdf" --flavour 2u --format text "$out"/*.pdf > "$report" || true
cat "$report"

passed=$(grep -c '^PASS ' "$report" || true)
echo "$passed of $count conform to PDF/A-2u"
[ "$passed" -eq "$count" ]
