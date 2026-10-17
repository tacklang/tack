package c

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A map, map[K]V, lowers to a struct of its own per map type, holding its
// entries in a buffer in the order their keys were first put, and an
// open-addressing table of slots that finds an entry by its key's hash:
//
//	typedef struct {
//	    K key;
//	    V val;
//	    uint64_t hash;
//	    bool live;
//	} tack_ment_9;
//
//	typedef struct {
//	    tack_ment_9 *entries; // insertion order, deleted ones not live
//	    int64_t len;          // how many entries are live
//	    int64_t used;         // how many of entries are used
//	    int64_t cap;          // how many entries has room for
//	    int64_t *slots;       // 0 empty, -1 deleted, or an entry's index + 1
//	    int64_t nslots;       // a power of two, or 0 while there are none
//	    tack_allocator alloc;
//	} tack_map_9;
//
// A slot is found by probing from the key's hash, one slot on at a time,
// until an empty one; a deleted slot is passed over, so a key past it is
// still found, and a key put after it may take it. Deleting an entry marks
// it not live and its slot deleted, so the rest keep their order and
// their slots, and a range skips it. Every used entry's slot stays filled,
// deleted or live, and used is kept at most three quarters of nslots, so
// a probe always meets an empty slot. When a new key would pass that, or
// the entries' room, tack_mgrow_ makes a new table of slots, twice as many
// as the live entries need, grows the entries' buffer when the live ones
// and the new one would not fit it, and only then, when both succeeded,
// moves the live entries down over the deleted ones, in order, and fills
// the new slots: a put that cannot grow leaves the map as it was. Both
// buffers come from the map's allocator, through std's tack_list_grow_
// and tack_list_free_, as a list's buffer does, so c_allocator counts and
// releases them.
//
// A key is hashed by tack_hash_ and its type's ID, by its value for an
// integer or a bool, and by its contents for every other key type, field
// by field, element by element or case and payload, so keys that == says
// are equal hash alike; hashes are deterministic, the same in every run.
// Keys are compared with ==, through the type's equality helper when it
// has one. tack_free_ and the map type's ID frees every live entry's key
// and value, first to last, when they are owned, then both buffers.

// mapName is the name of the C struct a map type lowers to.
func mapNameOf(typ types.Type) string {
	return fmt.Sprintf("tack_map_%d", typ)
}

// entryName is the name of the C struct of one entry of the map type typ.
func entryNameOf(typ types.Type) string {
	return fmt.Sprintf("tack_ment_%d", typ)
}

// hashName is the name of the helper that hashes a key of type typ.
func hashNameOf(typ types.Type) string {
	return fmt.Sprintf("tack_hash_%d", typ)
}

// mapHelper is the name of the map type typ's helper what: find, grow,
// put or take.
func mapHelperOf(what string, typ types.Type) string {
	return fmt.Sprintf("tack_m%s_%d", what, typ)
}

// emitMapDef defines the C structs a map type lowers to: its entry's and
// its own.
func (g *generator) emitMapDef(typ types.Type) {
	table := g.prog.Types
	fmt.Fprintf(&g.buf,
		"typedef struct {\n    %s key;\n    %s val;\n    uint64_t hash;\n    bool live;\n} %s;\n\n",
		g.cType(table.Key(typ)), g.cType(table.Elem(typ)), g.entryName(typ))
	fmt.Fprintf(
		&g.buf,
		"typedef struct {\n    %s *entries;\n    int64_t len;\n    int64_t used;\n    int64_t cap;\n"+
			"    int64_t *slots;\n    int64_t nslots;\n    tack_allocator alloc;\n} %s;\n\n",
		g.entryName(typ),
		g.mapName(typ),
	)
}

// mapTypes returns every map type the program has, in the order the
// checker added them.
func (g *generator) mapTypes() []types.Type {
	table := g.prog.Types
	var maps []types.Type
	for _, typ := range g.emittedTypes() {
		if table.IsMap(typ) {
			maps = append(maps, typ)
		}
	}
	return maps
}

// emitMaps writes, for every map type, the hash helper of its key type,
// after those of the types the key is made of, and its own helpers, once
// every free and equality helper they call is written. The mixing step
// every hash ends with comes first, when any map is used.
func (g *generator) emitMaps() {
	maps := g.mapTypes()
	if len(maps) == 0 {
		return
	}
	g.buf.WriteString("static uint64_t tack_mix_(uint64_t x) {\n" +
		"    x ^= x >> 30;\n    x *= 0xbf58476d1ce4e5b9u;\n    x ^= x >> 27;\n" +
		"    x *= 0x94d049bb133111ebu;\n    x ^= x >> 31;\n    return x;\n}\n\n")
	hashed := make(map[types.Type]bool)
	for _, typ := range maps {
		g.emitHashOf(g.prog.Types.Key(typ), hashed)
	}
	for _, typ := range maps {
		g.emitMapHelpers(typ)
	}
}

// emitHashOf writes the hash helper of typ, after those of the types it
// is made of, which it calls, unless hashed says it is written already.
func (g *generator) emitHashOf(typ types.Type, hashed map[types.Type]bool) {
	if hashed[typ] {
		return
	}
	hashed[typ] = true
	table := g.prog.Types
	if typ != types.String && !types.IsInteger(typ) && typ != types.Bool {
		for _, part := range g.hashedParts(typ) {
			g.emitHashOf(part, hashed)
		}
	}
	t := g.cType(typ)
	fmt.Fprintf(&g.buf, "static uint64_t %s(const %s *k) {\n", g.hashName(typ), t)
	switch {
	case typ == types.Bool, types.IsInteger(typ), typ == types.String:
		g.buf.WriteString(basicHash(typ))
	case table.IsArray(typ), table.IsList(typ):
		g.emitSequenceHash(typ)
	case table.IsUnion(typ):
		g.emitUnionHash(typ)
	default:
		g.emitStructHash(typ)
	}
	g.buf.WriteString("}\n\n")
}

// emitStructHash writes the body of the hash helper of a struct type:
// each field's hash, in order.
func (g *generator) emitStructHash(typ types.Type) {
	g.buf.WriteString("    uint64_t h = 0;\n")
	for _, f := range g.prog.Types.Fields(typ) {
		fmt.Fprintf(
			&g.buf,
			"    h = tack_mix_(h ^ %s(&k->%s));\n",
			g.hashName(f.Type),
			memberName(typ, f.Name),
		)
	}
	g.buf.WriteString("    (void)k;\n    return h;\n")
}

// basicHash returns the body of the hash helper of typ, a bool, an
// integer or a string: its value, widened to 64 bits, by value for a
// signed one, and mixed, or its bytes, through FNV-1a, and its length,
// mixed.
func basicHash(typ types.Type) string {
	switch {
	case typ == types.Bool:
		return "    return tack_mix_(*k ? 1u : 0u);\n"
	case types.IsInteger(typ) && types.IsUnsigned(typ):
		return "    return tack_mix_((uint64_t)*k);\n"
	case types.IsInteger(typ):
		return "    return tack_mix_((uint64_t)(int64_t)*k);\n"
	default:
		return "    uint64_t h = 14695981039346656037u;\n" +
			"    for (int64_t i = 0; i < k->len; i++) {\n        h ^= k->ptr[i];\n        h *= 1099511628211u;\n    }\n" +
			"    return tack_mix_(h ^ (uint64_t)k->len);\n"
	}
}

// hashedParts returns the types a key of type typ is made of, whose
// hashes its own is made of: an array's or a list's element type, a
// struct's field types, or a union's payloads' types.
func (g *generator) hashedParts(typ types.Type) []types.Type {
	table := g.prog.Types
	switch {
	case table.IsArray(typ), table.IsList(typ):
		return []types.Type{table.Elem(typ)}
	case table.IsUnion(typ):
		return g.payloads(typ)
	default:
		var parts []types.Type
		for _, f := range table.Fields(typ) {
			parts = append(parts, f.Type)
		}
		return parts
	}
}

// emitSequenceHash writes the body of the hash helper of an array or a
// list type: its length, then each element's hash, in order.
func (g *generator) emitSequenceHash(typ types.Type) {
	table := g.prog.Types
	field, n := "e", fmt.Sprint(table.Len(typ))
	if table.IsList(typ) {
		field, n = "ptr", "k->len"
	}
	fmt.Fprintf(&g.buf, "    uint64_t h = tack_mix_((uint64_t)%s);\n", n)
	fmt.Fprintf(
		&g.buf,
		"    for (int64_t i = 0; i < %s; i++) {\n        h = tack_mix_(h ^ %s(&k->%s[i]));\n    }\n",
		n,
		g.hashName(table.Elem(typ)),
		field,
	)
	g.buf.WriteString("    return h;\n")
}

// emitUnionHash writes the body of the hash helper of a union type: its
// tag, then the hash of the payload of the case it holds, when it has
// one.
func (g *generator) emitUnionHash(typ types.Type) {
	g.buf.WriteString("    uint64_t h = tack_mix_((uint64_t)k->tag);\n    switch (k->tag) {\n")
	for i, uc := range g.prog.Types.Cases(typ) {
		if uc.Payload {
			fmt.Fprintf(&g.buf, "    case %d:\n        return tack_mix_(h ^ %s(&k->u.%s));\n",
				i, g.hashName(uc.Type), caseMember(uc.Name))
		}
	}
	g.buf.WriteString("    default:\n        return h;\n    }\n")
}

// emitMapHelpers writes the map type typ's helpers: find, which gives the
// slot of a key, or -1; has, whether a key is there; at, the address of
// the value under a key proven there; grow, which makes room for one more
// entry, all or nothing; put, which puts a key and a value; and take,
// which takes the entry in a slot out, giving its value.
func (g *generator) emitMapHelpers(typ types.Type) {
	table := g.prog.Types
	key, value := table.Key(typ), table.Elem(typ)
	m, e, k, v := g.mapName(typ), g.entryName(typ), g.cType(key), g.cType(value)
	fmt.Fprintf(
		&g.buf,
		"static int64_t %s(const %s *m, const %s *k, uint64_t h) {\n",
		g.mapHelper("find", typ),
		m,
		k,
	)
	g.buf.WriteString("    if (m->nslots == 0) {\n        return -1;\n    }\n" +
		"    uint64_t mask = (uint64_t)m->nslots - 1;\n" +
		"    for (uint64_t i = h & mask;; i = (i + 1) & mask) {\n" +
		"        int64_t s = m->slots[i];\n        if (s == 0) {\n            return -1;\n        }\n")
	fmt.Fprintf(
		&g.buf,
		"        if (s > 0 && m->entries[s - 1].hash == h && %s) {\n            return (int64_t)i;\n        }\n    }\n}\n\n",
		g.equalExpr(key, "m->entries[s - 1].key", "(*k)"),
	)
	g.emitMapLookups(typ)
	g.emitMapGrow(typ)
	fmt.Fprintf(
		&g.buf,
		"%sstatic bool %s(%s *m, %s key, %s val) {\n",
		g.maybeUnused(g.mapHelper("put", typ)),
		g.mapHelper("put", typ),
		m,
		k,
		v,
	)
	fmt.Fprintf(
		&g.buf,
		"    uint64_t h = %s(&key);\n    int64_t s = %s(m, &key, h);\n",
		g.hashName(key),
		g.mapHelper("find", typ),
	)
	g.buf.WriteString("    if (s >= 0) {\n")
	fmt.Fprintf(&g.buf, "        %s *e = &m->entries[m->slots[s] - 1];\n", e)
	g.writeFrees("        ", value, "e->val")
	g.buf.WriteString("        e->val = val;\n")
	g.writeFrees("        ", key, "key")
	g.buf.WriteString("        return true;\n    }\n")
	fmt.Fprintf(&g.buf, "    if (!%s(m)) {\n", g.mapHelper("grow", typ))
	g.writeFrees("        ", key, "key")
	g.writeFrees("        ", value, "val")
	g.buf.WriteString("        return false;\n    }\n" +
		"    uint64_t mask = (uint64_t)m->nslots - 1;\n    uint64_t i = h & mask;\n" +
		"    while (m->slots[i] > 0) {\n        i = (i + 1) & mask;\n    }\n" +
		"    m->slots[i] = m->used + 1;\n")
	fmt.Fprintf(
		&g.buf,
		"    m->entries[m->used] = (%s){.key = key, .val = val, .hash = h, .live = true};\n",
		e,
	)
	g.buf.WriteString("    m->used++;\n    m->len++;\n    return true;\n}\n\n")
	g.emitMapTake(typ)
}

// emitMapTake writes the map type typ's take helper, which moves the
// value of the entry in slot s out, frees its key, and marks the entry
// and the slot deleted, clearing every slot once the map is empty.
func (g *generator) emitMapTake(typ types.Type) {
	key := g.prog.Types.Key(typ)
	m, e, v := g.mapName(typ), g.entryName(typ), g.cType(g.prog.Types.Elem(typ))
	take := g.mapHelper("take", typ)
	fmt.Fprintf(&g.buf, "%sstatic %s %s(%s *m, int64_t s) {\n", g.maybeUnused(take), v, take, m)
	fmt.Fprintf(&g.buf, "    %s *e = &m->entries[m->slots[s] - 1];\n    %s val = e->val;\n", e, v)
	g.writeFrees("    ", key, "e->key")
	g.buf.WriteString("    e->live = false;\n    m->slots[s] = -1;\n    m->len--;\n" +
		"    if (m->len == 0) {\n        m->used = 0;\n" +
		"        for (int64_t i = 0; i < m->nslots; i++) {\n            m->slots[i] = 0;\n        }\n    }\n" +
		"    return val;\n}\n\n")
}

// emitMapLookups writes the map type typ's has helper, which reports
// whether a key is in a map, and its at helper, which gives the address of
// the value under a key that the type checker has proven is there (see
// typeck's provenIn), as an element's index is proven in range: the
// address is used at once, by a read, an assignment or a mut argument,
// while the proof still holds, so no put or delete can have moved the
// entries or taken the key out. Each takes the key by value, a shallow
// copy that owns nothing, so a key that is not a place can be passed.
//
//	static bool tack_mhas_9(const tack_map_9 *m, int64_t key) {
//	    return tack_mfind_9(m, &key, tack_hash_1(&key)) >= 0;
//	}
//
//	static int64_t *tack_mat_9(const tack_map_9 *m, int64_t key) {
//	    return (int64_t *)&m->entries[m->slots[tack_mfind_9(m, &key, tack_hash_1(&key))] - 1].val;
//	}
func (g *generator) emitMapLookups(typ types.Type) {
	table := g.prog.Types
	m, k, v := g.mapName(typ), g.cType(table.Key(typ)), g.cType(table.Elem(typ))
	find := fmt.Sprintf(
		"%s(m, &key, %s(&key))",
		g.mapHelper("find", typ),
		g.hashName(table.Key(typ)),
	)
	has, at := g.mapHelper("has", typ), g.mapHelper("at", typ)
	fmt.Fprintf(&g.buf, "%sstatic bool %s(const %s *m, %s key) {\n    return %s >= 0;\n}\n\n",
		g.maybeUnused(has), has, m, k, find)
	fmt.Fprintf(&g.buf, "%sstatic %s *%s(const %s *m, %s key) {\n"+
		"    return (%s *)&m->entries[m->slots[%s] - 1].val;\n}\n\n",
		g.maybeUnused(at), v, at, m, k, v, find)
}

// emitMapGrow writes the map type typ's grow helper (see the comment at
// the top of this file). The slots' count stays below INT64_MAX / 4, so
// ns * 3 never overflows; tack_list_grow_ gives a buffer with no room
// before exactly the count asked for, a power of two of at least 8, when
// it has none to grow from, so the count it reports back is that power.
func (g *generator) emitMapGrow(typ types.Type) {
	m, e := g.mapName(typ), g.entryName(typ)
	fmt.Fprintf(&g.buf, "static bool %s(%s *m) {\n", g.mapHelper("grow", typ), m)
	g.buf.WriteString(
		"    if (m->used < m->cap && (m->used + 1) * 4 <= m->nslots * 3) {\n        return true;\n    }\n" +
			"    int64_t ns = 8;\n    while (ns * 3 < (m->len + 1) * 8) {\n" +
			"        if (ns > INT64_MAX / 8) {\n            return false;\n        }\n        ns *= 2;\n    }\n" +
			"    int64_t got = 0;\n" +
			"    int64_t *slots = tack_list_grow_(0, 0, ns, sizeof (int64_t), _Alignof (int64_t), m->alloc, &got);\n" +
			"    if (slots == 0) {\n        return false;\n    }\n" +
			"    ns = got;\n",
	)
	fmt.Fprintf(&g.buf, "    %s *entries = m->entries;\n    int64_t cap = m->cap;\n", e)
	g.buf.WriteString("    if (m->len + 1 > cap) {\n")
	fmt.Fprintf(
		&g.buf,
		"        entries = tack_list_grow_(m->entries, m->cap, m->len + 1, sizeof (%s), _Alignof (%s), m->alloc, &cap);\n",
		e,
		e,
	)
	g.buf.WriteString("        if (entries == 0) {\n" +
		"            tack_list_free_(slots, ns, sizeof (int64_t), _Alignof (int64_t), m->alloc);\n" +
		"            return false;\n        }\n    }\n" +
		"    int64_t used = 0;\n    for (int64_t i = 0; i < m->used; i++) {\n" +
		"        if (entries[i].live) {\n            entries[used++] = entries[i];\n        }\n    }\n" +
		"    for (int64_t i = 0; i < ns; i++) {\n        slots[i] = 0;\n    }\n" +
		"    uint64_t mask = (uint64_t)ns - 1;\n" +
		"    for (int64_t i = 0; i < used; i++) {\n        uint64_t j = entries[i].hash & mask;\n" +
		"        while (slots[j] != 0) {\n            j = (j + 1) & mask;\n        }\n        slots[j] = i + 1;\n    }\n" +
		"    tack_list_free_(m->slots, m->nslots, sizeof (int64_t), _Alignof (int64_t), m->alloc);\n" +
		"    m->entries = entries;\n    m->cap = cap;\n    m->used = used;\n    m->slots = slots;\n    m->nslots = ns;\n" +
		"    return true;\n}\n\n")
}

// writeFrees writes, each line indented by indent, the statements that
// free the value of type typ at the place ref, when typ is owned.
func (g *generator) writeFrees(indent string, typ types.Type, ref string) {
	if !g.owned(typ) {
		return
	}
	for _, line := range g.frees(typ, ref) {
		g.buf.WriteString(indent + line + "\n")
	}
}

// emitMapFree writes tack_free_ for the map type typ: it frees every live
// entry's key and value, first to last, when they are owned, then the
// entries' buffer and the slots'. A moved map is zeroed, so freeing it
// does nothing.
func (g *generator) emitMapFree(typ types.Type) {
	table := g.prog.Types
	key, value := table.Key(typ), table.Elem(typ)
	fmt.Fprintf(&g.buf, "static void %s(const %s *m) {\n", g.freeName(typ), g.mapName(typ))
	if g.owned(key) || g.owned(value) {
		g.buf.WriteString(
			"    for (int64_t i = 0; i < m->used; i++) {\n        if (m->entries[i].live) {\n",
		)
		g.writeFrees("            ", key, "m->entries[i].key")
		g.writeFrees("            ", value, "m->entries[i].val")
		g.buf.WriteString("        }\n    }\n")
	}
	fmt.Fprintf(
		&g.buf,
		"    tack_list_free_(m->entries, m->cap, sizeof (%s), _Alignof (%s), m->alloc);\n",
		g.entryName(typ),
		g.entryName(typ),
	)
	g.buf.WriteString(
		"    tack_list_free_(m->slots, m->nslots, sizeof (int64_t), _Alignof (int64_t), m->alloc);\n}\n\n",
	)
}

// makeMap renders make(map[K]V, alloc) as an empty map of its type
// holding the allocator, which allocates nothing.
func (g *generator) makeMap(call *ast.Call, alloc string) string {
	return fmt.Sprintf("((%s){.alloc = %s})", g.mapName(call.Type), alloc)
}

// putValue renders err := put(mut m, k, v) as a comma expression over
// temporaries at the function's top: the map's address, then the key and
// the value, moved when owned, each evaluated in order, then the call of
// the map type's put helper, its result an error: none, or code 1 with
// the message "out of memory" when the map could not grow, which leaves
// it as it was and frees the key and the value.
func (g *generator) putValue(call *ast.Call) (string, []diag.Diagnostic) {
	m, _ := call.Args[0].(*ast.MutArg)
	addr, diags := g.mutArg(m)
	if len(diags) > 0 {
		return "", diags
	}
	typ := ast.TypeOf(m.X)
	table := g.prog.Types
	key, diags := g.expr(call.Args[1], table.Key(typ))
	if len(diags) > 0 {
		return "", diags
	}
	val, diags := g.expr(call.Args[2], table.Elem(typ))
	if len(diags) > 0 {
		return "", diags
	}
	p, k, v := g.temp("o"), g.temp("o"), g.temp("o")
	g.frame.temps = append(
		g.frame.temps,
		fmt.Sprintf("%s *%s;", g.mapName(typ), p),
		fmt.Sprintf(
			"%s %s;",
			g.cType(table.Key(typ)),
			k,
		),
		fmt.Sprintf("%s %s;", g.cType(table.Elem(typ)), v),
	)
	return fmt.Sprintf("(%s = %s, %s = %s, %s = %s, %s(%s, %s, %s) ? (tack_error){} : %s)",
		p, addr, k, key, v, val, g.mapHelper("put", typ), p, k, v, g.listError(1, outOfMemory)), nil
}

// deleteValues lowers v, ok := delete(mut m, k): the map's address is held
// in a temporary, then the key, then the slot the key is in, found once,
// and the value, taken out only when there is one, the value type's zero
// value otherwise; ok is whether there is one.
func (g *generator) deleteValues(call *ast.Call) (multiSource, []diag.Diagnostic) {
	src, diags := g.mapKeySource(call.Args[0].(*ast.MutArg).X, call.Args[1], true)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	typ := ast.TypeOf(call.Args[0].(*ast.MutArg).X)
	zero := g.zeroValue(call.Type)
	// The value is taken into a temporary of its own, so the delete is
	// made even when the value is bound to _ and never read.
	src.temps = append(
		src.temps,
		multiTemp{typ: g.cType(call.Type), initFrom: func(r []string) string {
			return fmt.Sprintf(
				"%s >= 0 ? %s(%s, %s) : %s",
				r[2],
				g.mapHelper("take", typ),
				r[0],
				r[2],
				zero,
			)
		}},
	)
	src.field = func(tmps []string, i int) string {
		if i == 1 {
			// Read beside ok, so a value bound to _ is not an unused C
			// variable.
			return fmt.Sprintf("((void)%s, %s >= 0)", tmps[3], tmps[2])
		}
		return tmps[3]
	}
	return src, nil
}

// dropDelete lowers delete(mut m, k) written as a statement: the
// temporaries deleteValues holds, then, only when the key is in the map,
// the value taken out and dropped, freed when owned.
func (g *generator) dropDelete(call *ast.Call) ([]string, []diag.Diagnostic) {
	m := call.Args[0].(*ast.MutArg).X
	src, diags := g.mapKeySource(m, call.Args[1], true)
	if len(diags) > 0 {
		return nil, diags
	}
	names, lines := g.holdTemps(src)
	typ := ast.TypeOf(m)
	take := fmt.Sprintf("%s(%s, %s)", g.mapHelper("take", typ), names[0], names[2])
	body := []string{"(void)" + take + ";"}
	if g.owned(call.Type) {
		old := g.temp("r")
		body = append([]string{fmt.Sprintf("const %s %s = %s;", g.cType(call.Type), old, take)},
			g.frees(call.Type, old)...)
	}
	lines = append(lines, fmt.Sprintf("if (%s >= 0) {", names[2]))
	return append(append(lines, indent(body)...), "}"), nil
}

// lookupValues lowers v, ok := m[k], for a copyable value: the map's
// address is held, then the key, then its slot, and the value is read only
// when there is one, the value type's zero value given otherwise.
func (g *generator) lookupValues(ix *ast.Index) (multiSource, []diag.Diagnostic) {
	src, diags := g.mapKeySource(ix.X, ix.Index, false)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	zero := g.zeroValue(ix.Type)
	src.field = func(tmps []string, i int) string {
		if i == 1 {
			return tmps[2] + " >= 0"
		}
		return fmt.Sprintf("(%s >= 0 ? %s->entries[%s->slots[%s] - 1].val : %s)",
			tmps[2], tmps[0], tmps[0], tmps[2], zero)
	}
	return src, nil
}

// mapKeySource returns the temporaries a lookup of key in the map m
// holds, in order: m's address, a pointer to it, to a mutable map when
// mut is set; the key, read where it is, and borrowed, not moved, when
// owned; and the slot the key is in, or -1, found once.
func (g *generator) mapKeySource(m, key ast.Expr, mut bool) (multiSource, []diag.Diagnostic) {
	steps, place, diags := g.exprSteps(m, false)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	typ := ast.TypeOf(m)
	table := g.prog.Types
	k, diags := g.expr(key, table.Key(typ))
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	find := g.mapHelper("find", typ)
	hash := g.hashName(table.Key(typ))
	return multiSource{temps: []multiTemp{
		{typ: g.mapName(typ), init: withSteps(steps, "&"+place), ref: !mut, mut: mut},
		{typ: g.cType(table.Key(typ)), init: k},
		{typ: "int64_t", initFrom: func(r []string) string {
			return fmt.Sprintf("%s(%s, &%s, %s(&%s))", find, r[0], r[1], hash, r[1])
		}},
	}}, nil
}

// mapRange lowers a range loop over a map, whose lowered value is x, and
// lowered body body. The map is copied into a const temporary, which
// shares its buffers, and which every pass reads; the type checker keeps
// the body from changing the map while it runs, so neither buffer moves.
// The loop walks the used entries in order and skips those not live:
//
//	{
//	    const tack_map_9 tack_r0 = v_m;
//	    for (int64_t tack_k1 = 0; tack_k1 < tack_r0.used; tack_k1++) {
//	        if (!tack_r0.entries[tack_k1].live) {
//	            continue;
//	        }
//	        const int64_t v_k = tack_r0.entries[tack_k1].key;
//	        const int64_t v_v = tack_r0.entries[tack_k1].val;
//	        body
//	    }
//	}
//
// A key or a value of an owned type is bound in place, as a pointer to it,
// read through it, as a list's owned element is; generateForRange marks
// such a variable before the body is lowered.
func (g *generator) mapRange(loop *ast.ForRange, x string, body []string) []string {
	xType := ast.TypeOf(loop.X)
	r, k := g.temp("r"), g.temp("k")
	entry := fmt.Sprintf("%s.entries[%s]", r, k)
	vars := []string{fmt.Sprintf("if (!%s.live) {", entry), "    continue;", "}"}
	bind := func(v *ast.Var, member string) {
		if v == nil || v.Name == ast.Blank {
			return
		}
		if g.owned(v.Type) {
			vars = append(
				vars,
				fmt.Sprintf(
					"const %s *const %s = &%s.%s;",
					g.cType(v.Type),
					varName(v.Name),
					entry,
					member,
				),
			)
			return
		}
		vars = append(
			vars,
			fmt.Sprintf("const %s %s = %s.%s;", g.cType(v.Type), varName(v.Name), entry, member),
		)
	}
	bind(loop.Var, "key")
	bind(loop.Elem, "val")
	return rangeBlock(
		fmt.Sprintf("const %s %s = %s;", g.mapName(xType), r, x),
		fmt.Sprintf(
			"for (int64_t %s = 0; %s < %s.used; %s++) {",
			k,
			k,
			r,
			k,
		),
		append(vars, body...),
	)
}

// keyTypes returns the key types of every map the program has, which its
// helpers compare with ==, so their equality helpers are written.
func (g *generator) keyTypes() []types.Type {
	maps := g.mapTypes()
	keys := make([]types.Type, 0, len(maps))
	for _, typ := range maps {
		keys = append(keys, g.prog.Types.Key(typ))
	}
	return keys
}

// hasValue lowers has(m, k), m and k evaluated in order, to a call to
// the map type's has helper on m's address (see emitMapLookups). The
// checker makes m a place, since a map that is a temporary has no owner.
func (g *generator) hasValue(call *ast.Call) (string, []diag.Diagnostic) {
	m, key := call.Args[0], call.Args[1]
	rendered, diags := g.mapOperands(m, key)
	if len(diags) > 0 {
		return "", diags
	}
	has := g.mapHelper("has", ast.TypeOf(m))
	return g.sequenced([]ast.Expr{m, key}, rendered, func(ops []string) string {
		return fmt.Sprintf("%s(&%s, %s)", has, ops[0], ops[1])
	}), nil
}

// mapElementExpr lowers m[k], ix, read where k is proven in m, m and k
// evaluated in order, to the value the map type's at helper gives the
// address of (see emitMapLookups).
func (g *generator) mapElementExpr(ix *ast.Index) (string, []diag.Diagnostic) {
	rendered, diags := g.mapOperands(ix.X, ix.Index)
	if len(diags) > 0 {
		return "", diags
	}
	at := g.mapHelper("at", ast.TypeOf(ix.X))
	return g.sequenced([]ast.Expr{ix.X, ix.Index}, rendered, func(ops []string) string {
		return fmt.Sprintf("(*%s(&%s, %s))", at, ops[0], ops[1])
	}), nil
}

// mapOperands renders m, a map, and key, one of its keys, as the
// operands of a lookup: m parenthesized unless it is already one C
// operand, so its address can be taken.
func (g *generator) mapOperands(m, key ast.Expr) ([]string, []diag.Diagnostic) {
	base, diags := g.expr(m, types.Invalid)
	if len(diags) > 0 {
		return nil, diags
	}
	k, diags := g.expr(key, g.prog.Types.Key(ast.TypeOf(m)))
	if len(diags) > 0 {
		return nil, diags
	}
	return []string{"(" + base + ")", k}, nil
}

// mapElementRef renders m[k], ix, assigned or passed as mut where k is
// proven in m, as a C lvalue: the value the map type's at helper gives
// the address of, in base, the map rendered as a target (see targetRef).
func (g *generator) mapElementRef(ix *ast.Index, base string) (string, []diag.Diagnostic) {
	key, diags := g.expr(ix.Index, g.prog.Types.Key(ast.TypeOf(ix.X)))
	if len(diags) > 0 {
		return "", diags
	}
	return fmt.Sprintf("(*%s(&%s, %s))", g.mapHelper("at", ast.TypeOf(ix.X)), base, key), nil
}
