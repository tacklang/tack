package progen

import "errors"

// ctrl is how a statement ended: normally, or by a break, a continue or a
// return, which the enclosing loop or function takes.
type ctrl int

// The ways a statement ends.
const (
	cNormal ctrl = iota
	cBreak
	cContinue
	cReturn
)

// errBudget stops the interpreter when a program runs longer than its
// budget, which makes the program one to skip, not a failure.
var errBudget = errors.New("progen: the program runs past its step budget")

// machine is the interpreter's state for one run of a program: the
// checksum the program's obs calls fold values into, what it prints, and
// how many steps it has taken.
type machine struct {
	trace  []int64 // every value observed, in order, when tracing
	h      int64
	steps  int
	budget int
	traced bool
}

// step counts one step, and stops the run past the budget.
func (m *machine) step() {
	m.steps++
	if m.steps > m.budget {
		panic(errBudget)
	}
}

// observe folds v into the checksum, as the generated obs does: h = h *
// 31 + v, wrapping.
func (m *machine) observe(v int64) {
	m.h = m.h*31 + v
	if m.traced {
		m.trace = append(m.trace, v)
	}
}

// frame is one call's state: its variables, the values it returns, its
// result and its error, and the deferred code of the block running.
type frame struct {
	m      *machine
	vars   map[*Var]*Value
	defers []func(*frame)
	ret    Value
	retErr Value
}

// newFrame returns an empty frame on m.
func newFrame(m *machine) *frame {
	return &frame{m: m, vars: make(map[*Var]*Value)}
}

// get returns the value of v.
func (f *frame) get(v *Var) Value {
	return *f.vars[v]
}

// ref returns where v's value is held, for a write.
func (f *frame) ref(v *Var) *Value {
	return f.vars[v]
}

// set gives v the value val, declaring it if it is new.
func (f *frame) set(v *Var, val Value) {
	if p, ok := f.vars[v]; ok {
		*p = val
		return
	}
	f.vars[v] = &val
}

// execBlock runs stmts, a block, in order, stopping at the first that does
// not end normally, then the deferred code its defers registered, the
// last first, as Tack runs a block's defers when it is left by any path,
// and returns how it ended.
func execBlock(f *frame, stmts []Stmt) ctrl {
	saved := f.defers
	f.defers = nil
	c := cNormal
	for _, s := range stmts {
		f.m.step()
		if c = s.Exec(f); c != cNormal {
			break
		}
	}
	for i := len(f.defers) - 1; i >= 0; i-- {
		f.defers[i](f)
	}
	f.defers = saved
	return c
}

// runSeq runs stmts in order as one statement, which is no block, so
// registers no defers of its own.
func runSeq(f *frame, stmts []Stmt) ctrl {
	for _, s := range stmts {
		if c := s.Exec(f); c != cNormal {
			return c
		}
	}
	return cNormal
}

// call runs fn with args, values already evaluated left to right, and
// recv, the receiver of a method, nil for a function, and returns its
// result and its error: a new frame holds its parameters, a mut
// receiver referring to the caller's variable, and its body runs.
func call(m *machine, fn *Func, recv *Value, args []Value) (Value, Value) {
	m.step()
	f := newFrame(m)
	if fn.Recv != nil {
		if fn.Recv.Mut {
			f.vars[fn.Recv] = recv
		} else {
			f.set(fn.Recv, copyValue(*recv))
		}
	}
	for i, p := range fn.Params {
		f.set(p, args[i])
	}
	execBlock(f, fn.Body)
	return f.ret, f.retErr
}
