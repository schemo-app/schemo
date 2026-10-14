# non-matching entries

Everything in this directory is deliberately *not* a migration. `Load` must ignore all of it
rather than fail, so that a stray file next to a real set is harmless.

| file | why it must not match |
| --- | --- |
| `README.md` | not a .sql file at all |
| `notes.txt` | not a .sql file at all |
| `1_nopad.sql` | version is not zero-padded to four digits |
| `0002_init.sql.bak` | keeps a foreign suffix after .sql |
