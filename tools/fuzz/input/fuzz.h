// The C side of package input that tools/fuzz/entry.c shares with
// input.c: the input libFuzzer is running, which entry.c sets before it
// runs a target's main on it and clears after.
#ifndef TACK_FUZZ_INPUT_H
#define TACK_FUZZ_INPUT_H

#include <stddef.h>
#include <stdint.h>

// fuzz_set_input makes data, size bytes that stay valid until
// fuzz_clear_input is called, the input fuzz_input appends.
void fuzz_set_input(const uint8_t *data, size_t size);

// fuzz_clear_input leaves no input running, so fuzz_input
// fails until the next fuzz_set_input.
void fuzz_clear_input(void);

#endif
