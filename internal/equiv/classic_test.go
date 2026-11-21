package equiv_test

import "github.com/ambi/gamut/internal/mutant"

// classic は、テストが前提にする 5 つの変異演算子である。
var classic = []mutant.Type{mutant.ArithmeticBase, mutant.ConditionalsBoundary, mutant.ConditionalsNegation, mutant.IncrementDecrement, mutant.InvertNegatives}
