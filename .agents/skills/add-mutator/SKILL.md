---
name: add-mutator
description: Add a mutator to gamut, or change the replacements of an existing one. Use when asked to add a new kind of mutation or more mutators.
---

# Adding a mutator

1. Add a row to the [table of mutators in the design](../../../docs/design/mutants.md#mutators). Name it after Gremlins and gomutants where they have the same mutator.
2. Check that a replacement of one operator token expresses it. A mutation that cannot be expressed that way (statement removal, replacing return values) breaks the assumptions of the viability checks and of standalone builds, so rewrite the design's "representation of mutants" first.
3. Add mutating and non-mutating examples (constants, arguments of logging, generated files) to the table tests of enumeration in `internal/mutant`, see them fail, then implement the enumeration.
4. Add the rewritten text of an expression containing the mutator to the rewrite tests in `internal/schemata`. If the mutated operation can `panic` when recording infection, also add the branch that treats that condition as infection.
5. Add examples that end up both `KILLED` and `LIVED` to a fixed module in `testdata/`, and assert their statuses in an end-to-end test.
6. Pass `mise run verify`.
