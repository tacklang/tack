# Spec coverage for features.go

Spec: "The Go Programming Language Specification", language version go1.27 (May 26, 2026),
fetched from https://go.dev/ref/spec (identical headings to `$GOROOT/doc/go_spec.html` of go1.27.1).
Line numbers refer to `features.go`. Every one of the 159 spec headings (h2-h4) is listed in spec order.

Status: **covered** (code demonstrates it), **meta** (no language behavior to execute), **out of scope** (with reason).

| Heading | Status | Where |
|---|---|---|
| Introduction | meta | spec prose; noted in `introductionAndNotation` L91-94 |
| Notation | meta | EBNF grammar notation; noted in `introductionAndNotation` L91-94 |
| Source code representation | covered | `sourceCodeRepresentation` L100-117 |
| &nbsp;&nbsp;Characters | covered | `sourceCodeRepresentation` L100-117 (UTF-8 source, bytes vs code points) |
| &nbsp;&nbsp;Letters and digits | covered | `sourceCodeRepresentation` L100-117 (`_`, Arabic letters, Arabic-Indic digit in identifiers) |
| Lexical elements | covered | `lexicalElements` L123-133 |
| &nbsp;&nbsp;Comments | covered | L1-19 (line comments and a general comment before the package clause); `lexComments` L135-144 |
| &nbsp;&nbsp;Tokens | covered | `lexTokensAndSemicolons` L146-157; `lexIdentifiersKeywordsOperators` L165-182 |
| &nbsp;&nbsp;Semicolons | covered | `lexTokensAndSemicolons` L146-157; `lexComments` L135-144 (newline inside general comment) |
| &nbsp;&nbsp;Identifiers | covered | `sourceCodeRepresentation` L100-117; `lexIdentifiersKeywordsOperators` L165-182 |
| &nbsp;&nbsp;Keywords | covered | `lexIdentifiersKeywordsOperators` L165-182 (all 25 listed; all but `import` used in the file) |
| &nbsp;&nbsp;Operators and punctuation | covered | `lexIdentifiersKeywordsOperators` L165-182 (full list); `exprOperators` L1522-1551; `stmtSendIncDecAssignment` L1875-1915 (every op=) |
| &nbsp;&nbsp;Integer literals | covered | `lexIntegerLiterals` L184-194 |
| &nbsp;&nbsp;Floating-point literals | covered | `lexFloatingPointLiterals` L196-203 |
| &nbsp;&nbsp;Imaginary literals | covered | `lexImaginaryLiterals` L205-212 |
| &nbsp;&nbsp;Rune literals | covered | `lexRuneLiterals` L214-221 |
| &nbsp;&nbsp;String literals | covered | `lexStringLiterals` L223-237 |
| Constants | covered | `constants` L243-279; `exprConstantExpressions` L1732-1764 |
| Variables | covered | `variables` L290-307; `declVariables` L1067-1085 |
| Types | covered | `types` L313-332 |
| &nbsp;&nbsp;Boolean types | covered | `MyBool` L334; `typesBoolean` L336-340 |
| &nbsp;&nbsp;Numeric types | covered | `typesNumeric` L342-372 |
| &nbsp;&nbsp;String types | covered | `typesString` L374-382 |
| &nbsp;&nbsp;Array types | covered | `typesArray` L384-402 |
| &nbsp;&nbsp;Slice types | covered | `typesSlice` L404-418 |
| &nbsp;&nbsp;Struct types | covered | `Inner` L422-425; methods of `Inner` L427, L428, L429; `Base` L431; `Outer` L435-442; `typesStruct` L446-474 |
| &nbsp;&nbsp;Pointer types | covered | `typesPointer` L476-488 |
| &nbsp;&nbsp;Function types | covered | `typesFunction` L490-504 |
| &nbsp;&nbsp;Interface types | covered | `typesInterface` L506-512 |
| &nbsp;&nbsp;&nbsp;&nbsp;Basic interfaces | covered | `Reader` L515-518; `Writer` L519-522; methods of `file` L526, L527, L528; `interfacesBasic` L530-539 |
| &nbsp;&nbsp;&nbsp;&nbsp;Embedded interfaces | covered | `ReadWriter` L543-546; `interfacesEmbedded` L548-555 |
| &nbsp;&nbsp;&nbsp;&nbsp;General interfaces | covered | `Integer` L559-561; `Float` L562; `Number` L563-565; `Stringish` L566-569; `sumAll` L576-582; `interfacesGeneral` L584-590; `isEq` L592 |
| &nbsp;&nbsp;&nbsp;&nbsp;Implementing an interface | covered | `interfacesImplementing` L594-604 |
| &nbsp;&nbsp;Map types | covered | `typesMap` L606-622 |
| &nbsp;&nbsp;Channel types | covered | `typesChannel` L624-639 |
| Properties of types and values | covered | `propertiesOfTypesAndValues` L645-653 |
| &nbsp;&nbsp;Representation of values | covered | `propRepresentation` L655-669 |
| &nbsp;&nbsp;Underlying types | covered | `type (A1 … B4)` block L671-678; `propUnderlying` L680-687 |
| &nbsp;&nbsp;Type identity | covered | `propIdentity` L689-704 |
| &nbsp;&nbsp;Assignability | covered | `propAssignability` L706-717; `exprInstantiationAndInference` L1493-1520 (generic function assignable to func type, Go 1.27) |
| &nbsp;&nbsp;Representability | covered | `propRepresentability` L719-731 |
| &nbsp;&nbsp;Method sets | covered | `Counter` L733; methods of `Counter` L735, L736; `propMethodSets` L741-761; `interfacesImplementing` L594-604 |
| Blocks | covered | `blocks` L767-790 |
| Declarations and scope | covered | `declarationsAndScope` L796-810 |
| &nbsp;&nbsp;Label scopes | covered | `declLabelScopes` L812-825 |
| &nbsp;&nbsp;Blank identifier | covered | `declBlankIdentifier` L827-836 |
| &nbsp;&nbsp;Predeclared identifiers | covered | `declPredeclared` L840-855 |
| &nbsp;&nbsp;Exported identifiers | covered | `ExportedName` L162; `declExportedAndUniqueness` L857-863 |
| &nbsp;&nbsp;Uniqueness of identifiers | covered | `declExportedAndUniqueness` L857-863 (same-package case; the cross-package case needs a second package: out of scope) |
| &nbsp;&nbsp;Constant declarations | covered | `declConstants` L865-883 |
| &nbsp;&nbsp;Iota | covered | `Weekday` consts L887-891; `ByteSize` consts L895-900; `declIota` L902-936 |
| &nbsp;&nbsp;Type declarations | covered | `declTypes` L980-1005 |
| &nbsp;&nbsp;&nbsp;&nbsp;Alias declarations | covered | `Celsius` L939; `Set` L942 (generic alias); `declTypes` L980-1005 (local generic alias `pairOf`) |
| &nbsp;&nbsp;&nbsp;&nbsp;Type definitions | covered | `Temp` L947; `Temp2` L951; `Wrapped` L953; `Tree` L956-959; methods of `Tree` L961-971, L973-975, L978; `declTypes` L980-1005 |
| &nbsp;&nbsp;Type parameter declarations | covered | `firstOf` L1032; `PtrBox` L1036 (trailing-comma disambiguation); `PtrBox2` L1037; `BlankParam` L1039; `declTypeParameters` L1041-1058 |
| &nbsp;&nbsp;&nbsp;&nbsp;Type constraints | covered | `Ordered` L1009-1013; `Comparer` L1015; `maxBy` L1021-1029 (constraint referring to its own type parameter); `firstOf` L1032 (inline constraint) |
| &nbsp;&nbsp;&nbsp;&nbsp;Satisfying a type constraint | covered | `declTypeParameters` L1041-1058 (`any` satisfies `comparable`; run-time panic); `maxOrdered` L1060-1065 |
| &nbsp;&nbsp;Variable declarations | covered | `declVariables` L1067-1085 |
| &nbsp;&nbsp;Short variable declarations | covered | `declShortVariables` L1089-1113 |
| &nbsp;&nbsp;Function declarations | covered | `declFunctions` L1115-1124; `minInt` L1126-1131; `swapGeneric` L1133; `namedResults` L1135-1139; `terminating` L1143-1149; body-less (external) declarations need assembly in another file: out of scope |
| &nbsp;&nbsp;Method declarations | covered | methods of `Point` L1153, L1154, L1155; `Pair` L1157-1160; methods of `Pair` L1162, L1165; `List` L1167; methods of `List` L1170-1176 (generic method, Go 1.27); `declMethods` L1178-1190 |
| Expressions | covered | `expressions` L1196-1219 |
| &nbsp;&nbsp;Operands | covered | `exprOperandsAndQualified` L1221-1231 |
| &nbsp;&nbsp;Qualified identifiers | out of scope | requires an import; noted in `exprOperandsAndQualified` L1221-1231 |
| &nbsp;&nbsp;Composite literals | covered | `Line` L1235-1238; `Object` L1239; `Point3D` L1240-1243; `exprCompositeLiterals` L1245-1286 |
| &nbsp;&nbsp;Function literals | covered | `exprFunctionLiterals` L1288-1317 |
| &nbsp;&nbsp;Primary expressions | covered | `exprOperandsAndQualified` L1221-1231; `exprSelectors` L1335-1346; `exprIndex` L1377-1392; `exprSlices` L1396-1420; `exprCalls` L1437-1450 |
| &nbsp;&nbsp;Selectors | covered | `T0` L1319; `T1` L1323; `T2` L1327-1331; `exprSelectors` L1335-1346; `typesStruct` L446-474 (depth/shadowing, ambiguity) |
| &nbsp;&nbsp;Method expressions | covered | `TM` L1348; methods of `TM` L1350, L1351; `exprMethodExpressionsAndValues` L1353-1375 |
| &nbsp;&nbsp;Method values | covered | `exprMethodExpressionsAndValues` L1353-1375; `stmtRange` L2082-2177 (method value as iterator) |
| &nbsp;&nbsp;Index expressions | covered | `exprIndex` L1377-1392; `at` L1394 |
| &nbsp;&nbsp;Slice expressions | covered | `exprSlices` L1396-1420 |
| &nbsp;&nbsp;Type assertions | covered | `exprTypeAssertions` L1422-1435 |
| &nbsp;&nbsp;Calls | covered | `exprCalls` L1437-1450; `pairVals` L1452; `addPair` L1453 |
| &nbsp;&nbsp;Passing arguments to ... parameters | covered | `exprCalls` L1437-1450; `variadic` L1454-1459; `variadicMut` L1460 |
| &nbsp;&nbsp;Instantiations | covered | `exprInstantiationAndInference` L1493-1520; `Map` L1462-1468; `Double` L1470; `Zero` L1471; `Convert` L1472; `Identity` L1473 |
| &nbsp;&nbsp;Type inference | covered | `exprInstantiationAndInference` L1493-1520 |
| &nbsp;&nbsp;&nbsp;&nbsp;Type unification | covered | `Scale` L1475-1481; `Vec` L1483; `exprInstantiationAndInference` L1493-1520 (defined type preserved through `~[]E`) |
| &nbsp;&nbsp;Operators | covered | `exprOperators` L1522-1551 |
| &nbsp;&nbsp;&nbsp;&nbsp;Operator precedence | covered | `exprOperators` L1522-1551 |
| &nbsp;&nbsp;Arithmetic operators | covered | `exprArithmetic` L1553-1590 |
| &nbsp;&nbsp;&nbsp;&nbsp;Integer operators | covered | `exprArithmetic` L1553-1590 |
| &nbsp;&nbsp;&nbsp;&nbsp;Integer overflow | covered | `exprArithmetic` L1553-1590 |
| &nbsp;&nbsp;&nbsp;&nbsp;Floating-point operators | covered | `exprArithmetic` L1553-1590 (±Inf, NaN, FMA note) |
| &nbsp;&nbsp;&nbsp;&nbsp;String concatenation | covered | `exprArithmetic` L1553-1590 |
| &nbsp;&nbsp;Comparison operators | covered | `exprComparison` L1592-1625 |
| &nbsp;&nbsp;Logical operators | covered | `exprLogical` L1627-1634 |
| &nbsp;&nbsp;Address operators | covered | `exprAddress` L1636-1651 |
| &nbsp;&nbsp;Receive operator | covered | `exprReceive` L1653-1662 |
| &nbsp;&nbsp;Conversions | covered | `exprConversions` L1670-1696 |
| &nbsp;&nbsp;&nbsp;&nbsp;Conversions to and from a string type | covered | `exprStringConversions` L1698-1718 |
| &nbsp;&nbsp;&nbsp;&nbsp;Conversions from slice to array or array pointer | covered | `exprSliceToArray` L1720-1730 |
| &nbsp;&nbsp;Constant expressions | covered | `exprConstantExpressions` L1732-1764 |
| &nbsp;&nbsp;Order of evaluation | covered | `exprOrderOfEvaluation` L1766-1787; `stmtSelect` L2208-2268 (select operand evaluation) |
| Statements | covered | `statements` L1793-1810 |
| &nbsp;&nbsp;Terminating statements | covered | `stmtTerminating` L1812-1816; `term1` L1823; `term2` L1824-1831; `term3` L1832-1836; `term4` L1837-1842; `term5` L1843-1849; `term6` L1850 |
| &nbsp;&nbsp;Empty statements | covered | `stmtEmptyLabeledExpression` L1852-1873 |
| &nbsp;&nbsp;Labeled statements | covered | `stmtEmptyLabeledExpression` L1852-1873; `declLabelScopes` L812-825 |
| &nbsp;&nbsp;Expression statements | covered | `stmtEmptyLabeledExpression` L1852-1873 |
| &nbsp;&nbsp;Send statements | covered | `stmtSendIncDecAssignment` L1875-1915 |
| &nbsp;&nbsp;IncDec statements | covered | `stmtSendIncDecAssignment` L1875-1915 |
| &nbsp;&nbsp;Assignment statements | covered | `stmtSendIncDecAssignment` L1875-1915; `exprOrderOfEvaluation` L1766-1787 (two-phase assignment, swap) |
| &nbsp;&nbsp;If statements | covered | `stmtIf` L1917-1930 |
| &nbsp;&nbsp;Switch statements | covered | `stmtExpressionSwitch` L1932-1973; `stmtTypeSwitch` L2004-2014 |
| &nbsp;&nbsp;&nbsp;&nbsp;Expression switches | covered | `stmtExpressionSwitch` L1932-1973 |
| &nbsp;&nbsp;&nbsp;&nbsp;Type switches | covered | `classify` L1975-1993; `typeName` L1995-2002; `stmtTypeSwitch` L2004-2014 |
| &nbsp;&nbsp;For statements | covered | `stmtFor` L2016-2050; `stmtRange` L2082-2177 |
| &nbsp;&nbsp;&nbsp;&nbsp;For statements with single condition | covered | `stmtFor` L2016-2050 |
| &nbsp;&nbsp;&nbsp;&nbsp;For statements with for clause | covered | `stmtFor` L2016-2050 (incl. per-iteration variables); `exprFunctionLiterals` L1288-1317 (closures) |
| &nbsp;&nbsp;&nbsp;&nbsp;For statements with range clause | covered | `countTo` L2052-2060; `evens` L2062-2070; `enumerate` L2072-2080; `stmtRange` L2082-2177; `sumSlice` L2179-2185 |
| &nbsp;&nbsp;Go statements | covered | `stmtGo` L2187-2206 |
| &nbsp;&nbsp;Select statements | covered | `stmtSelect` L2208-2268 |
| &nbsp;&nbsp;Return statements | covered | `stmtReturn` L2270-2276; `ret1` L2278; `ret2` L2279; `ret3` L2281; `ret4` L2285-2291 |
| &nbsp;&nbsp;Break statements | covered | `stmtBreakContinue` L2293-2328 |
| &nbsp;&nbsp;Continue statements | covered | `stmtBreakContinue` L2293-2328 |
| &nbsp;&nbsp;Goto statements | covered | `stmtGoto` L2330-2344; `stmtEmptyLabeledExpression` L1852-1873 |
| &nbsp;&nbsp;Fallthrough statements | covered | `stmtFallthrough` L2346-2361 |
| &nbsp;&nbsp;Defer statements | covered | `stmtDefer` L2363-2369; `deferOrder` L2371-2379; `deferArgs` L2381-2389; `deferNamed` L2391-2394; `deferLoop` L2396-2404 |
| Built-in functions | covered | `builtinFunctions` L2410-2528 |
| &nbsp;&nbsp;Appending to and copying slices | covered | `sub("Appending to and copying slices")` block L2412-2435 |
| &nbsp;&nbsp;Clear | covered | `sub("Clear")` block L2436-2444; `clearGeneric` L2530 |
| &nbsp;&nbsp;Close | covered | `sub("Close")` block L2445-2455 |
| &nbsp;&nbsp;Manipulating complex numbers | covered | `sub("Manipulating complex numbers")` block L2456-2464 |
| &nbsp;&nbsp;Deletion of map elements | covered | `sub("Deletion of map elements")` block L2465-2472 |
| &nbsp;&nbsp;Length and capacity | covered | `sub("Length and capacity")` block L2473-2485 |
| &nbsp;&nbsp;Making slices, maps and channels | covered | `sub("Making slices, maps and channels")` block L2486-2497; `makeGeneric` L2532 |
| &nbsp;&nbsp;Min and max | covered | `sub("Min and max")` block L2498-2507 |
| &nbsp;&nbsp;Allocation | covered | `sub("Allocation")` block L2508-2517 (new(T) and new(expr)) |
| &nbsp;&nbsp;Handling panics | covered | `sub("Handling panics")` block L2518-2524; `safeDiv` L2534-2541; `recoverOnlyInDefer` L2543-2546; `repanic` L2548-2555; `panicNil` L2557-2567 |
| &nbsp;&nbsp;Bootstrapping | covered | `sub("Bootstrapping")` block L2525-2527 (all output in the file uses print/println) |
| Packages | covered | `packages` L2573-2585 |
| &nbsp;&nbsp;Source file organization | covered | L1-21 (header comments, package clause, no imports, then top-level decls); `sub("Source file organization")` block L2575-2578 |
| &nbsp;&nbsp;Package clause | covered | L21-21; `sub("Source file organization")` block L2575-2578 |
| &nbsp;&nbsp;Import declarations | out of scope | imports excluded by the task; noted in `sub("Import declarations")` block L2579-2580 |
| &nbsp;&nbsp;An example package | covered | `generate` L2587-2591; `filter` L2593-2599; `firstPrimes` L2601-2613 (spec prime sieve, run in-package) |
| Program initialization and execution | covered | `programInitializationAndExecution` L2619-2656 |
| &nbsp;&nbsp;The zero value | covered | `sub("The zero value")` block L2621-2647 |
| &nbsp;&nbsp;Package initialization | covered | L23-49 (dependency-ordered package vars, multi-value initializer); `sub("Package initialization")` block L2648-2655 |
| &nbsp;&nbsp;Program initialization | covered | `init` L47-49; `sub("Package initialization")` block L2648-2655 (order printed); multi-package init order is out of scope |
| &nbsp;&nbsp;Program execution | covered | `main` L56-76; `firstPrimes` L2601-2613 (main exits without waiting for blocked goroutines) |
| Errors | covered | `stringError` L2663; `codeError` L2669-2672; methods of `codeError` L2674, L2675; `mayFail` L2677-2682; `errorsChapter` L2684-2698 |
| Run-time panics | covered | `runtimeError` L2707-2710; `describe` L2712-2725; `recoverString` L2727-2731; `runTimePanics` L2733-2760 |
| System considerations | out of scope | needs `import "unsafe"`; noted in `systemConsiderations` L2766-2771 |
| &nbsp;&nbsp;Package unsafe | out of scope | requires importing `unsafe`, excluded by the task |
| &nbsp;&nbsp;Size and alignment guarantees | out of scope | only observable through unsafe.Sizeof/Alignof/Offsetof |
| Appendix | meta | version table and unification rules, mapped below |
| &nbsp;&nbsp;Language versions | meta | per-version features mapped below |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.9 | covered | alias declarations: `Celsius` L939 |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.13 | covered | 0b/0o prefixes, hex floats, any-base imaginary, `_` separators: `lexIntegerLiterals` L184-194, `lexFloatingPointLiterals` L196-203, `lexImaginaryLiterals` L205-212; signed shift counts: `exprOperators` L1522-1551 |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.14 | covered | duplicate methods via embedded interfaces (`Close` in Reader and Writer): `ReadWriter` L543-546 |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.17 | covered | slice to array pointer: `exprSliceToArray` L1720-1730. unsafe.Add/Slice: out of scope (unsafe) |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.18 | covered | generics, `~`, unions, `any`, `comparable`: `interfacesGeneral` L584-590, `Tree` L956-959, `declTypeParameters` L1041-1058 |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.20 | covered | slice to array: `exprSliceToArray` L1720-1730; comparable satisfied by interfaces: `declTypeParameters` L1041-1058. unsafe.SliceData/String/StringData: out of scope (unsafe) |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.21 | covered | min/max/clear: `builtinFunctions` L2410-2528; inference from generic function arguments and untyped constants: `exprInstantiationAndInference` L1493-1520 |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.22 | covered | per-iteration loop variables: `stmtFor` L2016-2050, `exprFunctionLiterals` L1288-1317; range over int: `stmtRange` L2082-2177 |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.23 | covered | range over function iterators: `stmtRange` L2082-2177 |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.24 | covered | generic alias: `Set` L942 |
| &nbsp;&nbsp;&nbsp;&nbsp;Go 1.27 | covered | assignment-context function inference: `exprInstantiationAndInference` L1493-1520; generic methods: methods of `List` L1170-1176, `exprMethodExpressionsAndValues` L1353-1375; promoted-field keys in struct literals: `exprCompositeLiterals` L1245-1286 |
| &nbsp;&nbsp;Type unification rules | covered | illustrated, not enumerated (the rules are compiler-internal): `exprInstantiationAndInference` L1493-1520, `Scale` L1475-1481 |

## Reconciliation with the task's starting list

- "Core type rules" was dropped: go1.27's spec no longer defines core types (the term does not appear anywhere in the go1.27 spec). Operations on type-parameter operands are shown under the rules the spec now states per operation: indexing (`at`), ranging (`sumSlice`), `make` (`makeGeneric`), `clear` (`clearGeneric`), conversions (`Convert`).
- Added beyond the starting list because the spec has them: generic methods, promoted-field keys in struct literals, and assignment-context function inference (all Go 1.27); `new(expr)`; label scopes; terminating statements; representation of values; representability; order of evaluation; the spec's prime-sieve example package.
- `print`/`println` write to standard error, so the trace appears on stderr.
