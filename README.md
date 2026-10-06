# meta-printer
The Meta-Printer is a background application that intercepts print-jobs, to inject some metadata (filename, filepath), into the document. This should be done in a temporary file, rather than modifying the original document. After the print job is completed, the temporary file can be deleted.

## Metadata placement by input type

- **Office/text sources (DOCX, ODT, DOC, RTF, plain text):** the temporary file is converted to a PDF via a headless LibreOffice instance, with the metadata inserted into a real Writer page-style header (Kopfzeile) that repeats on every page. Any existing header content is preserved, with the metadata inserted before it. Requires LibreOffice and python3 with UNO bindings on the print server (see [install/cups/install.sh](install/cups/install.sh)).
- **PDF sources:** a metadata cover page is prepended ahead of the original pages.
- **PostScript sources:** a metadata cover page is prepended ahead of the original document.

## Target printer

The physical printer is set in `/etc/meta-printer/target.json` (root-owned), with exactly one of `queue` (CUPS queue name) or `uri` (device URI):

```json
{"metaQueue": "MetaPrinter", "queue": "HP_LaserJet"}
```

`meta-printer-target.service` applies it to the MetaPrinter queue at boot, so end users never need `lpadmin` rights. After editing the file run `sudo systemctl restart meta-printer-target`.

Jobs printed from a GUI app (for example LibreOffice) arrive already rendered as PDF/PostScript, so they get the PDF/PostScript treatment; the true Writer header applies when an office file itself reaches the filter, e.g. `lp -d MetaPrinter file.odt`.


## Identity and Matching Strategy

The project uses a hybrid identity model to map print jobs to file-open events:

1. Daemon-side canonical identity: `(device id, inode)` captured from Linux file metadata when `metad` sees an `IN_OPEN` event.
2. Print-time correlation: content hash and recency when `metafilter` can read the source file.
3. Compatibility fallback: filename-based lookup when stronger signals are unavailable.

Why this is necessary:

- Hashes can change between open and print if a file is edited.
- Inode and device are usually stable across in-place edits and renames.
- CUPS does not reliably provide original source inode metadata to filters, so inode alone cannot solve print-time matching.

Current practical behavior in `metafilter`:

1. Try `(device id, inode)` lookup when the input path can be stat-ed.
2. If no inode match, try file-hash lookup.
3. If still no match, fall back to latest filename match.
