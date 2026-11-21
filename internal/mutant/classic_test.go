package mutant_test

import "github.com/ambi/gamut/internal/mutant"

// classic は、テストが前提にする 5 つの変異演算子である。既定の集合が変わっても、無効化コメントや重要領域を確かめるテストの期待は変えない。
var classic = []mutant.Type{mutant.ArithmeticBase, mutant.ConditionalsBoundary, mutant.ConditionalsNegation, mutant.IncrementDecrement, mutant.InvertNegatives}
