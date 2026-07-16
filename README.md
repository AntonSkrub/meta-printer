# meta-printer
The Meta-Printer is a background application that intercepts print-jobs, to inject some meta-data (filename, filepath), at the beginning of the document. This should be done in a temporary file, rather than modifying the original document. After the print job is completed, the temporary file can be deleted.
