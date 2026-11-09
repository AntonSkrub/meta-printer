# meta-printer
The Meta-Printer is a background application that intercepts print jobs and adds metadata (filename, filepath and print time) to the print output without modifying the original document.

## Supported print formats

- **PDF:** a metadata header is overlaid at the top of the first page. It can cover existing content in the top margin; the original document is not modified.
- **PostScript:** a PostScript metadata cover page is prepended ahead of the original document.

The original path is included when the filter can match the job to a daemon database record. Otherwise the CUPS job title is used as a fallback.

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


## Jumpstart Prompt

The idea is to implement an application that intercepts print jobs and adds a few lines, or a new page containing meta-data about the file (filename, filepath), at the beginning of the document. To ensure the original document remains unchanged, the application should create a temporary file that includes the meta-data and the original content. Once the print job is completed, the temporary file can be deleted to free up space and maintain privacy.

To improve the user experience, it would be great if this could then be registered as a default printer, so that users do not have to actively think about it. Should that not be possible, the application should at least be set up as a "named printer" that users can select when printing documents.

Some key requirements for the Meta-Printer application include:
- Use golang for implementation
- The application should be designed to run as a background service on a Linux (Debian 13) OS
- The application should be compatible with common document formats such as PDF, DOCX, and TXT.
    - It should also support popular applications like LibreOffice, Acrobat, and Mousepad.



Here is a rough draft of what the architecture of the Meta-Printer application might look like:
                  User
                    │
              Double-click file
                    │
        +--------------------------+
        | Metadata Daemon          |
        | (runs in background)     |
        +--------------------------+
          │                  │
          │ Launches app     │ Stores metadata
          ▼                  ▼
    LibreOffice          Metadata DB
    Acrobat              (SQLite)
    Mousepad
    ...
          │
          ▼
       Ctrl+P
          │
          ▼
    MetaPrinter (CUPS)
          │
          ▼
    Print Filter
          │
          ▼
    Looks up metadata
          │
          ▼
    Creates printable copy
          │
          ▼
      Real printer
