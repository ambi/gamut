package run_test

import "github.com/ambi/gamut/internal/mutant"

// classic は、テストが前提にする 5 つの変異演算子である。既定の集合が変わっても、判定の仕組みを確かめるテストの期待は変えない。
var classic = []mutant.Type{mutant.ArithmeticBase, mutant.ConditionalsBoundary, mutant.ConditionalsNegation, mutant.IncrementDecrement, mutant.InvertNegatives}
