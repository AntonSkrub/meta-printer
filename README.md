# meta-printer
The Meta-Printer is a background application that intercepts print-jobs, to inject some metadata (filename, filepath), into the document. This should be done in a temporary file, rather than modifying the original document. After the print job is completed, the temporary file can be deleted.

## Supported print formats

- **PDF:** a metadata cover page is prepended ahead of the original pages.
- **PostScript:** a PostScript metadata cover page is prepended ahead of the original document.

## Target printer

The physical printer is set in `/etc/meta-printer/target.json` (root-owned), with exactly one of `queue` (CUPS queue name) or `uri` (device URI):

```json
{"metaQueue": "MetaPrinter", "queue": "HP_LaserJet"}
```

`meta-printer-target.service` applies it to the MetaPrinter queue at boot, so end users never need `lpadmin` rights. After editing the file run `sudo systemctl restart meta-printer-target`.

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
