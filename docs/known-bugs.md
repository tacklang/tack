# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## Raw strings decode escape sequences

A backquoted string has its escape sequences decoded, with the double quote
as the extra escapable byte, so `` `\"` `` is a one-byte string, `` `\n` `` is
a newline, and `` `\`` `` is an invalid escape that does not end the string.
In Go a backquoted string is raw. Whether Tack's should be is still listed in
`docs/open-decisions.md` under "Escape sequences in raw strings"; this entry
records the current behavior until that is settled.
