# Checks for a future `tack vet`

Programs these checks would flag are valid Tack: they compile and do what
they say. Each check points out code that is legal but says more than it
needs to, or less than it seems to. None is implemented yet; `tack vet` does
not exist. Each entry gives what the check flags, the wording the compiler
once used when it was an error, and an example.

## An ok the compiler does not need

The checked form, `v, ok := ...`, is for an operation that can fail ([safety.md](safety.md)). Where the compiler can
already prove it cannot,
the form still compiles, and `ok` is always `true`. A vet check would
suggest the plain form instead. These were errors until the checked form
was allowed everywhere; the old wording is kept here for when the check is
written.

| Operation    | Flagged when                                                                            | Old wording                                             | Example                                              |
|--------------|-----------------------------------------------------------------------------------------|---------------------------------------------------------|------------------------------------------------------|
| division     | the divisor is a known value or proven not zero, or both operands are untyped constants | `the divisor cannot be zero here; remove the ok`          | `q, ok := n / 3`, `q, ok := 7 / 2`                   |
| array index  | the index is a known value, or proven in range                                          | `the index cannot be out of range here; remove the ok`    | `v, ok := a[1]`, or `a[i]` inside `for i := range a` |
| string index | the index is proven in range, a constant into a known string included                   | `the index cannot be out of range here; remove the ok`    | `v, ok := "tack"[1]`                                 |
| conversion   | the operand is an untyped constant that fits                                            | `a constant needs no ok to convert; use uint8(...)`     | `v, ok := uint8(5)`                                  |
| conversion   | the operand is a known value that fits                                                  | `a constant needs no ok to convert; use uint8(...)`     | `v, ok := uint8(x)` after `x := 5`                   |
| conversion   | the operand's type widens to the target type, the same type included                    | `converting uint8 to int64 cannot fail; use int64(...)` | `v, ok := int64(b)` for a `uint8` `b`                |

A failure the compiler can already see is still an error, in the checked
form as in the plain one: a divisor known to be zero, a constant index out
of range, and a constant or known value that does not fit the type it is
converted to.
