package c

import "testing"

// TestNotPlace checks notPlace on C an addressable expression may render
// to: a comma, a conditional or an assignment that gives the value is
// found through any grouping parentheses, and one inside a call's
// arguments, an index, an initializer or a character or string literal
// is not, and C cut short, as no rendering is, ends the scan without a
// fault.
func TestNotPlace(t *testing.T) {
	for _, tt := range []struct {
		c    string
		want bool
	}{
		{"v_p", false},
		{"v_p.f_a", false},
		{"(*p_x).f_a", false},
		{"v_rows.e[0]", false},
		{"v_l.ptr[tack_o1]", false},
		{"(tack_S){.f_a = 1, .f_b = 2}", false},
		{"((tack_U){.tag = 0, .u.c_pair = (o0 = f(1), (S){.f_a = o0})})", false},
		{"v_a.e[tack_div(x, y)]", false},
		{"v_a.e[(o0 = f(1), o0)]", false},
		{"f(a, b).f_x", false},
		{"g(h(a, b), c)[0].f_x", false},
		{"v_s.e[','].f_x", false},
		{`v_s.e['\'', 0]`, false},
		{"", false},
		{"'", false},
		{`v_s.e["\`, false},
		{"(a, '", true},
		{"))", false},
		{"(o0 = f(1), o1 = f(2), (S){.f_a = o0, .f_b = o1})", true},
		{"((o0 = f(1), (S){.f_a = o0}))", true},
		{"(o0 = f(1), (S){.f_a = o0}).f_s", true},
		{"(c ? v_a : v_b)", true},
		{"(q = v_a)", true},
		{"v_a == v_b", true},
		{"(f(a, b), v_p)", true},
		{"((v_p, v_q))", true},
	} {
		if got := notPlace(tt.c); got != tt.want {
			t.Errorf("notPlace(%q) = %v, want %v", tt.c, got, tt.want)
		}
	}
}
